{
  description = "spind docker image template";

  nixConfig = {
    extra-substituters = [ "https://microvm.cachix.org" ];
    extra-trusted-public-keys = [ "microvm.cachix.org-1:oXnBc6hRE3eX5rSYdRyMYXnfzcCxC7yKPTbZXALsqys=" ];
  };

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    microvm = {
      url = "github:microvm-nix/microvm.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, microvm }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      goArch = system:
        {
          x86_64-linux = "amd64";
          aarch64-linux = "arm64";
        }.${system};
      guestBinaryNames = [
        "spind-guest-agent"
      ];
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          lib = pkgs.lib;

          prebuiltGuestBinariesAvailable =
            builtins.all (name: builtins.pathExists (./guest-binaries + "/${name}")) guestBinaryNames;

          spindGuestAgentFromTemplate = pkgs.runCommand "spind-guest-agent-0.0.0" { } ''
            mkdir -p "$out/bin"
            ${lib.concatMapStringsSep "\n" (name:
              "install -m 0755 ${./guest-binaries + "/${name}"} \"$out/bin/${name}\""
            ) guestBinaryNames}
          '';

          spindGuestAgent =
            if prebuiltGuestBinariesAvailable
            then spindGuestAgentFromTemplate
            else builtins.throw "spind docker template requires guest-binaries; build it with spind image build docker";

          dockerDataDisk = {
            image = "docker-data.img";
            mountPoint = "/var/lib/docker";
            size = 8192;
            label = "spind-docker";
            fsType = "ext4";
            autoCreate = false;
          };

          nixos = nixpkgs.lib.nixosSystem {
            inherit system;
            modules = [
              microvm.nixosModules.microvm
              ({ config, lib, pkgs, ... }: {
                system.stateVersion = lib.trivial.release;

                microvm = {
                  hypervisor = "cloud-hypervisor";
                  vcpu = 2;
                  mem = 4096;
                  storeOnDisk = true;
                  storeDiskType = "erofs";
                  volumes = [ dockerDataDisk ];
                };

                boot.initrd.availableKernelModules = [
                  "erofs"
                  "ext4"
                  "overlay"
                  "virtio_blk"
                  "virtio_console"
                  "virtio_net"
                  "virtio_pci"
                  "vmw_vsock_virtio_transport"
                ];
                boot.kernelModules = [
                  "br_netfilter"
                  "overlay"
                  "vmw_vsock_virtio_transport"
                ];
                boot.kernel.sysctl = {
                  "net.ipv4.ip_forward" = 1;
                  "net.ipv4.ping_group_range" = "0 2147483647";
                };

                networking.hostName = "spind-docker";
                networking.firewall.enable = false;
                networking.useDHCP = false;
                systemd.network.networks."10-spind-uplink" = {
                  matchConfig.Name = "en* eth*";
                  networkConfig = {
                    DHCP = "yes";
                    IPv6AcceptRA = true;
                  };
                  linkConfig.RequiredForOnline = "routable";
                };

                users.groups.spind.gid = 1000;
                users.users.spind = {
                  isNormalUser = true;
                  uid = 1000;
                  group = "spind";
                  home = "/home/spind";
                  extraGroups = [ "docker" ];
                };

                services.openssh = {
                  enable = true;
                  settings = {
                    PasswordAuthentication = false;
                    PermitRootLogin = "prohibit-password";
                    PubkeyAuthentication = true;
                  };
                };

                virtualisation.docker = {
                  enable = true;
                  enableOnBoot = true;
                  storageDriver = "overlay2";
                };

                environment.systemPackages = [
                  pkgs.iproute2
                  pkgs.kmod
                  pkgs.util-linux
                  spindGuestAgent
                ];

                systemd.services.spind-authorized-key = {
                  description = "Install spind SSH authorized key";
                  wantedBy = [ "multi-user.target" ];
                  before = [ "sshd.service" ];
                  requiredBy = [ "sshd.service" ];
                  serviceConfig.Type = "oneshot";
                  path = [ pkgs.coreutils ];
                  script = ''
                    set -euo pipefail
                    key=""
                    for field in $(cat /proc/cmdline); do
                      case "$field" in
                        spind.ssh_authorized_key=*) key="''${field#spind.ssh_authorized_key=}" ;;
                      esac
                    done
                    if [ -z "$key" ]; then
                      echo "missing spind.ssh_authorized_key kernel parameter" >&2
                      exit 1
                    fi
                    install -d -m 0700 -o spind -g spind /home/spind/.ssh
                    printf '%s' "$key" | base64 --decode > /home/spind/.ssh/authorized_keys.tmp
                    chown spind:spind /home/spind/.ssh/authorized_keys.tmp
                    chmod 0600 /home/spind/.ssh/authorized_keys.tmp
                    mv /home/spind/.ssh/authorized_keys.tmp /home/spind/.ssh/authorized_keys
                  '';
                };

                systemd.services.spind-vsock-ssh-proxy = {
                  description = "spind SSH vsock proxy";
                  wantedBy = [ "multi-user.target" ];
                  requires = [ "sshd.service" ];
                  after = [ "sshd.service" ];
                  path = [ pkgs.iproute2 pkgs.kmod ];
                  preStart = ''
                    modprobe vmw_vsock_virtio_transport 2>/dev/null || true
                    ip link set lo up 2>/dev/null || true
                  '';
                  serviceConfig = {
                    ExecStart = "${spindGuestAgent}/bin/spind-guest-agent ssh-proxy";
                    Restart = "always";
                    RestartSec = "1s";
                  };
                };

                systemd.services.spind-vsock-docker-proxy = {
                  description = "spind Docker vsock proxy";
                  wantedBy = [ "multi-user.target" ];
                  wants = [ "docker.service" ];
                  after = [ "docker.service" ];
                  path = [ pkgs.iproute2 pkgs.kmod ];
                  preStart = ''
                    modprobe vmw_vsock_virtio_transport 2>/dev/null || true
                    ip link set lo up 2>/dev/null || true
                  '';
                  serviceConfig = {
                    ExecStart = "${spindGuestAgent}/bin/spind-guest-agent docker-proxy";
                    Restart = "always";
                    RestartSec = "1s";
                  };
                };

                systemd.services.spind-vsock-tcp-forward-proxy = {
                  description = "spind TCP forward vsock proxy";
                  wantedBy = [ "multi-user.target" ];
                  after = [ "network.target" ];
                  path = [ pkgs.kmod ];
                  preStart = ''
                    modprobe vmw_vsock_virtio_transport 2>/dev/null || true
                  '';
                  serviceConfig = {
                    ExecStart = "${spindGuestAgent}/bin/spind-guest-agent tcp-forward-proxy";
                    Restart = "always";
                    RestartSec = "1s";
                  };
                };

                systemd.services.spind-vsock-mount-helper = {
                  description = "spind mount helper";
                  wantedBy = [ "multi-user.target" ];
                  after = [ "network.target" ];
                  path = [ pkgs.kmod pkgs.util-linux ];
                  preStart = ''
                    modprobe vmw_vsock_virtio_transport 2>/dev/null || true
                  '';
                  serviceConfig = {
                    ExecStart = "${spindGuestAgent}/bin/spind-guest-agent mount-helper";
                    Restart = "always";
                    RestartSec = "1s";
                  };
                };
              })
            ];
          };

          kernelPath =
            if system == "x86_64-linux" then
              "${nixos.config.microvm.kernel.dev}/vmlinux"
            else
              "${nixos.config.microvm.kernel.out}/${pkgs.stdenv.hostPlatform.linux-kernel.target}";

          kernelCommandLine = lib.concatStringsSep " " ([
            "earlyprintk=ttyS0"
            "console=ttyS0"
            "reboot=t"
            "panic=-1"
          ] ++ nixos.config.microvm.kernelParams);

          spindMicrovmDockerImage = pkgs.runCommand "spind-microvm-docker-image" { } ''
            mkdir -p "$out"
            cp ${kernelPath} "$out/kernel"
            cp ${nixos.config.microvm.initrdPath} "$out/initramfs"
            cp ${nixos.config.microvm.storeDisk} "$out/nix-store.img"
            cat > "$out/metadata.json" <<'JSON'
            ${builtins.toJSON {
              name = "docker";
              imageType = "microvm-nix";
              architecture = goArch system;
              execUser = "spind";
              cpuCount = 2;
              memoryMiB = 4096;
              kernelCommandLine = kernelCommandLine;
              sshAuthorizedKeyCommandLineParam = "spind.ssh_authorized_key";
              disks = [
                {
                  name = "nix-store.img";
                  readOnly = true;
                  imageType = "raw";
                }
                {
                  name = dockerDataDisk.image;
                  readOnly = false;
                  imageType = "raw";
                  create = {
                    size = "${toString dockerDataDisk.size}MiB";
                    fsType = dockerDataDisk.fsType;
                    label = dockerDataDisk.label;
                  };
                }
              ];
            }}
            JSON
          '';
        in
        {
          default = spindMicrovmDockerImage;
        });
    };
}
