import Foundation
import Virtualization
import Darwin

signal(SIGPIPE, SIG_IGN)

var activeExecRelay: ExecRelayServer?
var activeDockerRelay: ExecRelayServer?
var activeTCPForwardRelay: ExecRelayServer?
var activeHostShareRelay: ExecRelayServer?
var activeControlServer: ControlServer?
var stopSignalSource: DispatchSourceSignal?

struct RunnerConfig: Decodable {
    let name: String
    let kernelPath: String
    let initramfsPath: String
    let diskPath: String?
    let disks: [RunnerDisk]?
    let logPath: String
    let execSocketPath: String
    let controlSocketPath: String?
    let restoreStatePath: String?
    let machineIdentifier: String?
    let execPort: UInt32
    let dockerSocketPath: String?
    let dockerPort: UInt32?
    let tcpForwardPath: String?
    let tcpForwardPort: UInt32?
    let networkMac: String?
    let hostSharePath: String?
    let hostShareTag: String?
    let hostShareSocketPath: String?
    let hostSharePort: UInt32?
    let cpuCount: Int
    let memoryMiB: Int
    let kernelCommandLine: String
}

struct RunnerDisk: Decodable {
    let path: String
    let readOnly: Bool?
}

struct ControlRequest: Decodable {
    let op: String
    let statePath: String?
}

struct ControlResponse: Encodable {
    let ok: Bool
    let error: String?
}

struct LifecycleEvent: Encodable {
    let event: String
}

enum RunnerError: Error, CustomStringConvertible {
    case usage(String)
    case missingFile(String)
    case invalidPID(String)
    case socket(String)

    var description: String {
        switch self {
        case .usage(let message):
            return message
        case .missingFile(let path):
            return "missing file: \(path)"
        case .invalidPID(let value):
            return "invalid pid: \(value)"
        case .socket(let message):
            return message
        }
    }
}

func main() throws {
    let args = Array(CommandLine.arguments.dropFirst())
    guard let command = args.first else {
        throw RunnerError.usage("usage: spind-vz machine-id | validate --config <path> | start --config <path> | restore --config <path> | stop --pid <pid>")
    }

    switch command {
    case "machine-id":
        if #available(macOS 13.0, *) {
            let identifier = VZGenericMachineIdentifier()
            FileHandle.standardOutput.write(Data("\(identifier.dataRepresentation.base64EncodedString())\n".utf8))
        } else {
            throw RunnerError.usage("machine-id requires macOS 13 or newer")
        }
    case "validate":
        let config = try readConfig(from: optionValue("--config", in: args))
        try validateFiles(config)
    case "start":
        let config = try readConfig(from: optionValue("--config", in: args))
        try validateFiles(config)
        try startVM(config)
    case "restore":
        let config = try readConfig(from: optionValue("--config", in: args))
        try validateFiles(config)
        try restoreVM(config)
    case "stop":
        let pidValue = try optionValue("--pid", in: args)
        guard let pid = Int32(pidValue) else {
            throw RunnerError.invalidPID(pidValue)
        }
        kill(pid, SIGTERM)
    default:
        throw RunnerError.usage("unknown command: \(command)")
    }
}

func optionValue(_ name: String, in args: [String]) throws -> String {
    guard let index = args.firstIndex(of: name), index + 1 < args.count else {
        throw RunnerError.usage("missing \(name)")
    }
    return args[index + 1]
}

func readConfig(from path: String) throws -> RunnerConfig {
    let data = try Data(contentsOf: URL(fileURLWithPath: path))
    return try JSONDecoder().decode(RunnerConfig.self, from: data)
}

func validateFiles(_ config: RunnerConfig) throws {
    var paths = [config.kernelPath, config.initramfsPath]
    if let diskPath = config.diskPath, !diskPath.isEmpty {
        paths.append(diskPath)
    }
    for disk in config.disks ?? [] {
        paths.append(disk.path)
    }
    if let restoreStatePath = config.restoreStatePath, !restoreStatePath.isEmpty {
        paths.append(restoreStatePath)
    }
    for path in paths {
        if !FileManager.default.fileExists(atPath: path) {
            throw RunnerError.missingFile(path)
        }
    }
}

func makeVMConfiguration(_ config: RunnerConfig) throws -> VZVirtualMachineConfiguration {
    let vmConfig = VZVirtualMachineConfiguration()
    vmConfig.cpuCount = max(1, config.cpuCount)
    vmConfig.memorySize = UInt64(max(512, config.memoryMiB)) * 1024 * 1024

    let bootLoader = VZLinuxBootLoader(kernelURL: URL(fileURLWithPath: config.kernelPath))
    bootLoader.initialRamdiskURL = URL(fileURLWithPath: config.initramfsPath)
    bootLoader.commandLine = config.kernelCommandLine
    vmConfig.bootLoader = bootLoader
    let platform = VZGenericPlatformConfiguration()
    if #available(macOS 13.0, *) {
        if let encodedMachineIdentifier = config.machineIdentifier, !encodedMachineIdentifier.isEmpty {
            guard let data = Data(base64Encoded: encodedMachineIdentifier),
                  let machineIdentifier = VZGenericMachineIdentifier(dataRepresentation: data) else {
                throw RunnerError.usage("invalid machineIdentifier in config")
            }
            platform.machineIdentifier = machineIdentifier
        }
    }
    vmConfig.platform = platform

    var storageDevices: [VZVirtioBlockDeviceConfiguration] = []
    if let diskPath = config.diskPath, !diskPath.isEmpty {
        let diskAttachment = try VZDiskImageStorageDeviceAttachment(
            url: URL(fileURLWithPath: diskPath),
            readOnly: false
        )
        storageDevices.append(VZVirtioBlockDeviceConfiguration(attachment: diskAttachment))
    }
    for disk in config.disks ?? [] {
        let attachment = try VZDiskImageStorageDeviceAttachment(
            url: URL(fileURLWithPath: disk.path),
            readOnly: disk.readOnly ?? false
        )
        storageDevices.append(VZVirtioBlockDeviceConfiguration(attachment: attachment))
    }
    if storageDevices.isEmpty {
        throw RunnerError.usage("config must include diskPath or disks")
    }
    vmConfig.storageDevices = storageDevices

    vmConfig.entropyDevices = [VZVirtioEntropyDeviceConfiguration()]
    vmConfig.socketDevices = [VZVirtioSocketDeviceConfiguration()]
    let network = VZVirtioNetworkDeviceConfiguration()
    if let networkMac = config.networkMac, !networkMac.isEmpty {
        guard let macAddress = VZMACAddress(string: networkMac) else {
            throw RunnerError.usage("invalid networkMac in config")
        }
        network.macAddress = macAddress
    }
    network.attachment = VZNATNetworkDeviceAttachment()
    vmConfig.networkDevices = [network]

    if let hostSharePath = config.hostSharePath, !hostSharePath.isEmpty {
        let tag = (config.hostShareTag?.isEmpty == false) ? config.hostShareTag! : "spind-share"
        if #available(macOS 13.0, *) {
            let sharedDirectory = VZSharedDirectory(url: URL(fileURLWithPath: hostSharePath), readOnly: false)
            let directoryShare = VZSingleDirectoryShare(directory: sharedDirectory)
            let fileSystem = VZVirtioFileSystemDeviceConfiguration(tag: tag)
            fileSystem.share = directoryShare
            vmConfig.directorySharingDevices = [fileSystem]
        } else {
            throw RunnerError.usage("host directory sharing requires macOS 13 or newer")
        }
    }

    let nullInput = FileHandle(forReadingAtPath: "/dev/null") ?? FileHandle.standardInput
    if !FileManager.default.fileExists(atPath: config.logPath) {
        FileManager.default.createFile(atPath: config.logPath, contents: nil)
    }
    let serialOutput = FileHandle(forWritingAtPath: config.logPath) ?? FileHandle.standardOutput
    serialOutput.seekToEndOfFile()
    let serial = VZVirtioConsoleDeviceSerialPortConfiguration()
    serial.attachment = VZFileHandleSerialPortAttachment(
        fileHandleForReading: nullInput,
        fileHandleForWriting: serialOutput
    )
    vmConfig.serialPorts = [serial]

    try vmConfig.validate()
    return vmConfig
}

func startVM(_ config: RunnerConfig) throws {
    let vmConfig = try makeVMConfiguration(config)
    if #available(macOS 14.0, *) {
        try vmConfig.validateSaveRestoreSupport()
    }
    let machine = VZVirtualMachine(configuration: vmConfig)
    guard let socketDevice = machine.socketDevices.compactMap({ $0 as? VZVirtioSocketDevice }).first else {
        throw RunnerError.socket("virtio socket device is not available")
    }
    let execRelay = try ExecRelayServer(
        path: config.execSocketPath,
        socketDevice: socketDevice,
        port: config.execPort,
        framed: false,
        connectToGuest: false
    )
    execRelay.activateSocketListener()
    execRelay.start()
    activeExecRelay = execRelay
    try startDockerRelayIfConfigured(config, socketDevice: socketDevice)
    try startTCPForwardRelayIfConfigured(config, socketDevice: socketDevice)
    try startHostShareRelayIfConfigured(config, socketDevice: socketDevice)

    if let controlSocketPath = config.controlSocketPath, !controlSocketPath.isEmpty {
        let controlServer = try ControlServer(path: controlSocketPath, machine: machine)
        controlServer.start()
        activeControlServer = controlServer
    }

    installStopSignalHandler(machine)

    machine.start { result in
        if case .failure(let error) = result {
            logError("failed to start virtual machine: \(error)")
            exit(1)
        }
    }

    dispatchMain()
}

func restoreVM(_ config: RunnerConfig) throws {
    guard let restoreStatePath = config.restoreStatePath, !restoreStatePath.isEmpty else {
        throw RunnerError.usage("missing restoreStatePath in config")
    }
    let vmConfig = try makeVMConfiguration(config)
    if #available(macOS 14.0, *) {
        try vmConfig.validateSaveRestoreSupport()
    } else {
        throw RunnerError.usage("saved state restore requires macOS 14 or newer")
    }

    let machine = VZVirtualMachine(configuration: vmConfig)
    installStopSignalHandler(machine)

    if #available(macOS 14.0, *) {
        machine.restoreMachineStateFrom(url: URL(fileURLWithPath: restoreStatePath)) { error in
            if let error {
                logError("failed to restore virtual machine: \(error)")
                exit(1)
            }
            emitLifecycleEvent("restore-completed")
            do {
                guard let socketDevice = machine.socketDevices.compactMap({ $0 as? VZVirtioSocketDevice }).first else {
                    throw RunnerError.socket("virtio socket device is not available")
                }
                let execRelay = try ExecRelayServer(
                    path: config.execSocketPath,
                    socketDevice: socketDevice,
                    port: config.execPort,
                    framed: false,
                    connectToGuest: false
                )
                execRelay.activateSocketListener()
                execRelay.start()
                activeExecRelay = execRelay
                try startDockerRelayIfConfigured(config, socketDevice: socketDevice)
                try startTCPForwardRelayIfConfigured(config, socketDevice: socketDevice)
                try startHostShareRelayIfConfigured(config, socketDevice: socketDevice)

                if let controlSocketPath = config.controlSocketPath, !controlSocketPath.isEmpty {
                    let controlServer = try ControlServer(path: controlSocketPath, machine: machine)
                    controlServer.start()
                    activeControlServer = controlServer
                }
            } catch {
                logError("failed to initialize restored virtual machine relay: \(error)")
                exit(1)
            }
            machine.resume { resumeResult in
                switch resumeResult {
                case .success:
                    emitLifecycleEvent("resume-completed")
                    emitLifecycleEvent("exec-relay-ready")
                case .failure(let error):
                    logError("failed to resume restored virtual machine: \(error)")
                    exit(1)
                }
            }
        }
    }

    dispatchMain()
}

func startDockerRelayIfConfigured(_ config: RunnerConfig, socketDevice: VZVirtioSocketDevice) throws {
    guard let dockerSocketPath = config.dockerSocketPath, !dockerSocketPath.isEmpty else {
        return
    }
    let dockerRelay = try ExecRelayServer(
        path: dockerSocketPath,
        socketDevice: socketDevice,
        port: config.dockerPort ?? 10240,
        framed: true,
        connectToGuest: false
    )
    dockerRelay.activateSocketListener()
    dockerRelay.start()
    activeDockerRelay = dockerRelay
}

func startTCPForwardRelayIfConfigured(_ config: RunnerConfig, socketDevice: VZVirtioSocketDevice) throws {
    guard let tcpForwardPath = config.tcpForwardPath, !tcpForwardPath.isEmpty else {
        return
    }
    let tcpForwardRelay = try ExecRelayServer(
        path: tcpForwardPath,
        socketDevice: socketDevice,
        port: config.tcpForwardPort ?? 10241,
        framed: false,
        connectToGuest: false
    )
    tcpForwardRelay.activateSocketListener()
    tcpForwardRelay.start()
    activeTCPForwardRelay = tcpForwardRelay
}

func startHostShareRelayIfConfigured(_ config: RunnerConfig, socketDevice: VZVirtioSocketDevice) throws {
    guard let hostShareSocketPath = config.hostShareSocketPath, !hostShareSocketPath.isEmpty else {
        return
    }
    let hostShareRelay = try ExecRelayServer(
        path: hostShareSocketPath,
        socketDevice: socketDevice,
        port: config.hostSharePort ?? 10242,
        framed: false,
        connectToGuest: false
    )
    hostShareRelay.activateSocketListener()
    hostShareRelay.start()
    activeHostShareRelay = hostShareRelay
}

final class ControlServer {
    private let listenFD: Int32
    private let machine: VZVirtualMachine

    init(path: String, machine: VZVirtualMachine) throws {
        self.machine = machine
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        if fd < 0 {
            throw RunnerError.socket("create control socket failed")
        }
        self.listenFD = fd

        unlink(path)
        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        let maxPathLength = MemoryLayout.size(ofValue: address.sun_path)
        guard path.utf8.count < maxPathLength else {
            close(fd)
            throw RunnerError.socket("control socket path is too long: \(path)")
        }
        _ = path.withCString { source in
            withUnsafeMutablePointer(to: &address.sun_path) { destination in
                destination.withMemoryRebound(to: CChar.self, capacity: maxPathLength) { bytes in
                    strncpy(bytes, source, maxPathLength)
                }
            }
        }

        let bindResult = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPointer in
                Darwin.bind(fd, sockaddrPointer, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        if bindResult != 0 {
            let message = String(cString: strerror(errno))
            close(fd)
            throw RunnerError.socket("bind control socket failed: \(message)")
        }
        if listen(fd, 4) != 0 {
            let message = String(cString: strerror(errno))
            close(fd)
            throw RunnerError.socket("listen control socket failed: \(message)")
        }
    }

    deinit {
        close(listenFD)
    }

    func start() {
        DispatchQueue.global(qos: .userInitiated).async {
            while true {
                let localFD = accept(self.listenFD, nil, nil)
                if localFD < 0 {
                    continue
                }
                self.handle(localFD: localFD)
            }
        }
    }

    private func handle(localFD: Int32) {
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                let data = try readLine(from: localFD)
                let request = try JSONDecoder().decode(ControlRequest.self, from: data)
                guard request.op == "save" else {
                    writeControlResponse(ControlResponse(ok: false, error: "unsupported op: \(request.op)"), to: localFD)
                    close(localFD)
                    return
                }
                guard let statePath = request.statePath, !statePath.isEmpty else {
                    writeControlResponse(ControlResponse(ok: false, error: "missing statePath"), to: localFD)
                    close(localFD)
                    return
                }
                self.saveState(to: statePath) { result in
                    switch result {
                    case .success:
                        self.stopMachine { stopResult in
                            switch stopResult {
                            case .success:
                                writeControlResponse(ControlResponse(ok: true, error: nil), to: localFD)
                            case .failure(let error):
                                writeControlResponse(ControlResponse(ok: false, error: "\(error)"), to: localFD)
                            }
                            close(localFD)
                            DispatchQueue.main.async {
                                exit(0)
                            }
                        }
                    case .failure(let error):
                        writeControlResponse(ControlResponse(ok: false, error: "\(error)"), to: localFD)
                        close(localFD)
                    }
                }
            } catch {
                writeControlResponse(ControlResponse(ok: false, error: "\(error)"), to: localFD)
                close(localFD)
            }
        }
    }

    private func saveState(to path: String, completion: @escaping (Result<Void, Error>) -> Void) {
        DispatchQueue.main.async {
            if #available(macOS 14.0, *) {
                self.machine.pause { pauseResult in
                    switch pauseResult {
                    case .success:
                        self.machine.saveMachineStateTo(url: URL(fileURLWithPath: path)) { error in
                            if let error {
                                completion(.failure(error))
                            } else {
                                completion(.success(()))
                            }
                        }
                    case .failure(let error):
                        completion(.failure(error))
                    }
                }
            } else {
                completion(.failure(RunnerError.usage("saved state save requires macOS 14 or newer")))
            }
        }
    }

    private func stopMachine(completion: @escaping (Result<Void, Error>) -> Void) {
        DispatchQueue.main.async {
            self.machine.stop { error in
                if let error {
                    completion(.failure(error))
                } else {
                    completion(.success(()))
                }
            }
        }
    }
}

final class ExecRelayServer: NSObject, VZVirtioSocketListenerDelegate {
    private let listenFD: Int32
    private let socketDevice: VZVirtioSocketDevice
    private let socketListener: VZVirtioSocketListener
    private let port: UInt32
    private let framed: Bool
    private let connectToGuest: Bool
    private let guestConnectionLock = NSLock()
    private let guestConnectionReady = DispatchSemaphore(value: 0)
    private var guestConnections: [VZVirtioSocketConnection] = []

    init(path: String, socketDevice: VZVirtioSocketDevice, port: UInt32, framed: Bool, connectToGuest: Bool) throws {
        self.socketDevice = socketDevice
        self.socketListener = VZVirtioSocketListener()
        self.port = port
        self.framed = framed
        self.connectToGuest = connectToGuest

        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        if fd < 0 {
            throw RunnerError.socket("create exec relay socket failed")
        }
        self.listenFD = fd

        unlink(path)
        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        let maxPathLength = MemoryLayout.size(ofValue: address.sun_path)
        guard path.utf8.count < maxPathLength else {
            close(fd)
            throw RunnerError.socket("exec relay socket path is too long: \(path)")
        }
        _ = path.withCString { source in
            withUnsafeMutablePointer(to: &address.sun_path) { destination in
                destination.withMemoryRebound(to: CChar.self, capacity: maxPathLength) { bytes in
                    strncpy(bytes, source, maxPathLength)
                }
            }
        }

        let bindResult = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPointer in
                Darwin.bind(fd, sockaddrPointer, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        if bindResult != 0 {
            let message = String(cString: strerror(errno))
            close(fd)
            throw RunnerError.socket("bind exec relay socket failed: \(message)")
        }
        if listen(fd, 16) != 0 {
            let message = String(cString: strerror(errno))
            close(fd)
            throw RunnerError.socket("listen exec relay socket failed: \(message)")
        }
        super.init()
        socketListener.delegate = self
    }

    deinit {
        if !connectToGuest {
            socketDevice.removeSocketListener(forPort: port)
        }
        close(listenFD)
    }

    func activateSocketListener() {
        if connectToGuest {
            return
        }
        socketDevice.setSocketListener(socketListener, forPort: port)
    }

    func start() {
        DispatchQueue.global(qos: .userInitiated).async {
            while true {
                let localFD = accept(self.listenFD, nil, nil)
                if localFD < 0 {
                    continue
                }
                self.handle(localFD: localFD)
            }
        }
    }

    private func handle(localFD: Int32) {
        DispatchQueue.global(qos: .userInitiated).async {
            if self.connectToGuest {
                self.socketDevice.connect(toPort: self.port) { result in
                    switch result {
                    case .success(let connection):
                        self.relay(localFD: localFD, connection: connection)
                    case .failure(let error):
                        logError("connect relay guest port \(self.port) failed: \(error)")
                        close(localFD)
                    }
                }
                return
            }
            let connection = self.nextGuestConnection()
            self.relay(localFD: localFD, connection: connection)
        }
    }

    func listener(
        _ listener: VZVirtioSocketListener,
        shouldAcceptNewConnection connection: VZVirtioSocketConnection,
        from socketDevice: VZVirtioSocketDevice
    ) -> Bool {
        guestConnectionLock.lock()
        guestConnections.append(connection)
        guestConnectionLock.unlock()
        guestConnectionReady.signal()
        return true
    }

    private func nextGuestConnection() -> VZVirtioSocketConnection {
        while true {
            guestConnectionReady.wait()
            guestConnectionLock.lock()
            if !guestConnections.isEmpty {
                let connection = guestConnections.removeFirst()
                guestConnectionLock.unlock()
                return connection
            }
            guestConnectionLock.unlock()
        }
    }

    private func relay(localFD: Int32, connection: VZVirtioSocketConnection) {
        let remoteFD = connection.fileDescriptor
        if framed {
            relayFramed(localFD: localFD, remoteFD: remoteFD, connection: connection)
            return
        }
        let group = DispatchGroup()
        group.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            if let error = copyFileDescriptor(from: localFD, to: remoteFD) {
                logError("copy exec relay host-to-guest failed: \(error)")
            }
            shutdown(remoteFD, SHUT_WR)
            group.leave()
        }
        group.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            if let error = copyFileDescriptor(from: remoteFD, to: localFD) {
                logError("copy exec relay guest-to-host failed: \(error)")
            }
            shutdown(localFD, SHUT_WR)
            group.leave()
        }
        group.notify(queue: .global(qos: .utility)) {
            connection.close()
            close(localFD)
        }
    }

    private func relayFramed(localFD: Int32, remoteFD: Int32, connection: VZVirtioSocketConnection) {
        let group = DispatchGroup()
        group.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            if let error = copyLocalToFramed(localFD: localFD, framedFD: remoteFD) {
                logError("copy framed relay host-to-guest failed: \(error)")
            }
            group.leave()
        }
        group.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            if let error = copyFramedToLocal(framedFD: remoteFD, localFD: localFD) {
                logError("copy framed relay guest-to-host failed: \(error)")
            }
            group.leave()
        }
        group.notify(queue: .global(qos: .utility)) {
            connection.close()
            close(localFD)
        }
    }
}

func copyFileDescriptor(from sourceFD: Int32, to destinationFD: Int32) -> String? {
    var buffer = [UInt8](repeating: 0, count: 16 * 1024)
    while true {
        let readCount = buffer.withUnsafeMutableBytes { rawBuffer in
            read(sourceFD, rawBuffer.baseAddress, rawBuffer.count)
        }
        if readCount == 0 {
            return nil
        }
        if readCount < 0 {
            if errno == EINTR {
                continue
            }
            return String(cString: strerror(errno))
        }
        var written = 0
        while written < readCount {
            let writeCount = buffer.withUnsafeBytes { rawBuffer in
                write(destinationFD, rawBuffer.baseAddress!.advanced(by: written), readCount - written)
            }
            if writeCount == 0 {
                return nil
            }
            if writeCount < 0 {
                if errno == EINTR {
                    continue
                }
                return String(cString: strerror(errno))
            }
            written += writeCount
        }
    }
}

let streamRelayFrameData = UInt8(0)
let streamRelayFrameCloseWrite = UInt8(1)
let streamRelayMaxFramePayload = 64 * 1024

func copyLocalToFramed(localFD: Int32, framedFD: Int32) -> String? {
    var buffer = [UInt8](repeating: 0, count: streamRelayMaxFramePayload)
    while true {
        let readCount = buffer.withUnsafeMutableBytes { rawBuffer in
            read(localFD, rawBuffer.baseAddress, rawBuffer.count)
        }
        if readCount > 0 {
            if let error = writeFrame(to: framedFD, frameType: streamRelayFrameData, payload: Array(buffer[..<readCount])) {
                return error
            }
        }
        if readCount == 0 {
            return writeFrame(to: framedFD, frameType: streamRelayFrameCloseWrite, payload: [])
        }
        if readCount < 0 {
            if errno == EINTR {
                continue
            }
            return String(cString: strerror(errno))
        }
    }
}

func copyFramedToLocal(framedFD: Int32, localFD: Int32) -> String? {
    while true {
        let result = readFrame(from: framedFD)
        if let error = result.error {
            return error
        }
        guard let frame = result.frame else {
            return "missing stream relay frame"
        }
        if frame.type == streamRelayFrameData {
            if frame.payload.isEmpty {
                continue
            }
            if let error = writeFull(to: localFD, frame.payload) {
                return error
            }
        } else if frame.type == streamRelayFrameCloseWrite {
            shutdown(localFD, SHUT_WR)
            return nil
        } else {
            return "unknown stream relay frame type \(frame.type)"
        }
    }
}

func writeFrame(to fd: Int32, frameType: UInt8, payload: [UInt8]) -> String? {
    if payload.count > streamRelayMaxFramePayload {
        return "stream relay frame payload too large: \(payload.count)"
    }
    var header = [UInt8](repeating: 0, count: 5)
    header[0] = frameType
    let size = UInt32(payload.count).bigEndian
    withUnsafeBytes(of: size) { rawBuffer in
        for index in 0..<4 {
            header[index + 1] = rawBuffer[index]
        }
    }
    if let error = writeFull(to: fd, header) {
        return error
    }
    if payload.isEmpty {
        return nil
    }
    return writeFull(to: fd, payload)
}

func readFrame(from fd: Int32) -> (frame: (type: UInt8, payload: [UInt8])?, error: String?) {
    var header = [UInt8](repeating: 0, count: 5)
    if let error = readFull(from: fd, into: &header) {
        return (nil, error)
    }
    let size = header[1...4].reduce(UInt32(0)) { ($0 << 8) | UInt32($1) }
    if size > UInt32(streamRelayMaxFramePayload) {
        return (nil, "stream relay frame payload too large: \(size)")
    }
    var payload = [UInt8](repeating: 0, count: Int(size))
    if size > 0 {
        if let error = readFull(from: fd, into: &payload) {
            return (nil, error)
        }
    }
    return ((type: header[0], payload: payload), nil)
}

func readFull(from fd: Int32, into buffer: inout [UInt8]) -> String? {
    var offset = 0
    let totalCount = buffer.count
    while offset < totalCount {
        let remaining = totalCount - offset
        let readCount = buffer.withUnsafeMutableBytes { rawBuffer in
            read(fd, rawBuffer.baseAddress!.advanced(by: offset), remaining)
        }
        if readCount == 0 {
            return "unexpected EOF"
        }
        if readCount < 0 {
            if errno == EINTR {
                continue
            }
            return String(cString: strerror(errno))
        }
        offset += readCount
    }
    return nil
}

func writeFull(to fd: Int32, _ buffer: [UInt8]) -> String? {
    var written = 0
    while written < buffer.count {
        let writeCount = buffer.withUnsafeBytes { rawBuffer in
            write(fd, rawBuffer.baseAddress!.advanced(by: written), buffer.count - written)
        }
        if writeCount == 0 {
            return nil
        }
        if writeCount < 0 {
            if errno == EINTR {
                continue
            }
            return String(cString: strerror(errno))
        }
        written += writeCount
    }
    return nil
}

func readLine(from fd: Int32) throws -> Data {
    var data = Data()
    var byte = [UInt8](repeating: 0, count: 1)
    while true {
        let count = read(fd, &byte, 1)
        if count == 0 {
            break
        }
        if count < 0 {
            if errno == EINTR {
                continue
            }
            throw RunnerError.socket("read control socket failed: \(String(cString: strerror(errno)))")
        }
        data.append(byte[0])
        if byte[0] == UInt8(ascii: "\n") {
            break
        }
    }
    return data
}

func writeControlResponse(_ response: ControlResponse, to fd: Int32) {
    do {
        let data = try JSONEncoder().encode(response) + Data("\n".utf8)
        data.withUnsafeBytes { rawBuffer in
            guard let baseAddress = rawBuffer.baseAddress else {
                return
            }
            var written = 0
            while written < data.count {
                let count = write(fd, baseAddress.advanced(by: written), data.count - written)
                if count <= 0 {
                    if errno == EINTR {
                        continue
                    }
                    return
                }
                written += count
            }
        }
    } catch {
        let fallback = "{\"ok\":false,\"error\":\"encode response failed\"}\n"
        _ = fallback.withCString { pointer in
            write(fd, pointer, strlen(pointer))
        }
    }
}

func emitLifecycleEvent(_ event: String) {
    do {
        let data = try JSONEncoder().encode(LifecycleEvent(event: event)) + Data("\n".utf8)
        FileHandle.standardOutput.write(data)
    } catch {
        logError("failed to encode lifecycle event: \(error)")
    }
}

func logError(_ message: String) {
    FileHandle.standardError.write(Data("spind-vz: \(message)\n".utf8))
}

func installStopSignalHandler(_ machine: VZVirtualMachine) {
    signal(SIGTERM, SIG_IGN)
    let source = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
    source.setEventHandler {
        requestStop(machine)
    }
    source.resume()
    stopSignalSource = source
}

func requestStop(_ machine: VZVirtualMachine) {
    if machine.canRequestStop {
        do {
            try machine.requestStop()
        } catch {
            machine.stop { _ in
                exit(0)
            }
            return
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1) {
            machine.stop { _ in
                exit(0)
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                exit(0)
            }
        }
    } else {
        machine.stop { _ in
            exit(0)
        }
    }
}

do {
    try main()
} catch {
    FileHandle.standardError.write(Data("spind-vz: \(error)\n".utf8))
    exit(1)
}
