import { $ as execa$ } from "execa";
import { randomBytes } from "node:crypto";
import { once } from "node:events";
import { access, mkdir, mkdtemp, rm } from "node:fs/promises";
import { type Socket, createConnection } from "node:net";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { e2eEnv } from "./env";

const create$ = execa$({ env: { PATH: e2eEnv.path } });

const createVerbose$ = create$({ verbose: e2eEnv.execaVerbose });

const CLOUD_HYPERVISOR_BACKEND = "cloud-hypervisor";
export const DOCKER_HOST_PREREQUISITES = "docker-host";
export const KIND_READY_PREREQUISITES = "kind-ready";
export const K3D_READY_PREREQUISITES = "k3d-ready";

export type E2EPrerequisites =
  | typeof DOCKER_HOST_PREREQUISITES
  | typeof KIND_READY_PREREQUISITES
  | typeof K3D_READY_PREREQUISITES
  | "vm";

export class E2EEnv {
  /** Short random id shared by resources in this test run. */
  readonly id: string;
  /** Base VM name, for example `e2e-kind-base-1a2b3c4d`. */
  readonly baseVM: string;
  /** Kind cluster name, for example `e2e-1a2b3c4d`. */
  readonly clusterName: string;
  /** Kubernetes context name, for example `kind-e2e-1a2b3c4d`. */
  readonly context: string;
  /** Kubeconfig path, for example `/tmp/kido-e2e/kubeconfig`. */
  readonly kubeconfig: string;
  /**
   * Restored kubeconfig path, for example
   * `/tmp/kido-e2e/spind-home/vms/e2e-kind-work-1a2b3c4d/kubeconfig`.
   */
  readonly restoredKubeconfig: string;
  /** Snapshot name, for example `e2e-kind-ready-1a2b3c4d`. */
  readonly snapshot: string;
  /** Work VM name, for example `e2e-kind-work-1a2b3c4d`. */
  readonly workVM: string;

  readonly #root: string;
  readonly #spindHome: string;

  #cleaned = false;

  constructor(root: string) {
    const id = randomBytes(4).toString("hex");
    this.id = id;
    this.#root = root;
    this.#spindHome = path.join(root, "spind-home");
    this.kubeconfig = path.join(root, "kubeconfig");
    this.baseVM = `e2e-kind-base-${id}`;
    this.workVM = `e2e-kind-work-${id}`;
    this.snapshot = `e2e-kind-ready-${id}`;
    this.clusterName = `e2e-${id}`;
    this.context = `kind-${this.clusterName}`;
    this.restoredKubeconfig = path.join(
      this.#spindHome,
      "vms",
      this.workVM,
      "kubeconfig",
    );
  }

  get #env(): Record<string, string | undefined> {
    return { SPIND_HOME: this.#spindHome, SPIND_IMAGE_STORE: imageStore() };
  }

  get root(): string {
    return this.#root;
  }

  get spindHome(): string {
    return this.#spindHome;
  }

  get #baseVMEnv(): Record<string, string | undefined> {
    return {
      ...this.#env,
      DOCKER_HOST: `unix://${path.join(this.#spindHome, "vms", this.baseVM, "docker.sock")}`,
      KUBECONFIG: this.kubeconfig,
    };
  }

  get $() {
    return createVerbose$({ env: this.#baseVMEnv });
  }

  get spind$() {
    return createVerbose$({ env: this.#env });
  }

  docker$(vmName: string) {
    return createVerbose$({
      env: {
        ...this.#env,
        DOCKER_HOST: `unix://${path.join(this.#spindHome, "vms", vmName, "docker.sock")}`,
      },
    });
  }

  project$(cwd: string) {
    return createVerbose$({ cwd, env: this.#env });
  }

  cleanup$() {
    return create$({ env: this.#env, reject: false, timeout: 120_000 });
  }

  async [Symbol.asyncDispose](): Promise<void> {
    await this.#cleanup();
  }

  async #cleanup(): Promise<void> {
    if (this.#cleaned) {
      return;
    }
    this.#cleaned = true;
    const $ = create$({
      env: this.#baseVMEnv,
      reject: false,
      timeout: 120_000,
    });
    await $`kind delete cluster --name ${this.clusterName}`;
    await $`spind vm delete ${this.workVM} --force`;
    await $`spind vm delete ${this.baseVM} --force`;
    await $`spind snapshot delete ${this.snapshot}`;
    await rm(this.#root, { recursive: true, force: true });
  }
}

export async function createE2EEnv(
  prerequisites: E2EPrerequisites = KIND_READY_PREREQUISITES,
): Promise<E2EEnv> {
  const root = await mkdtempInE2ERoot();
  const backend = detectBackend();
  await mkdir(path.join(root, "spind-home"), { recursive: true });
  await requireE2EPrerequisites(backend, prerequisites);
  const env = new E2EEnv(root);
  return env;
}

export async function waitForTCPPort({
  host,
  port,
  timeoutMs,
}: {
  readonly host: string;
  readonly port: number;
  readonly timeoutMs: number;
}): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  const lastError = await waitForTCPPortAttempt({ deadline, host, port });

  if (lastError !== undefined) {
    const endpoint = `${host}:${port.toString()}`;
    throw new Error(`TCP port ${endpoint} was not ready: ${lastError}`);
  }
}

async function requireE2EPrerequisites(
  backend: string,
  prerequisites: E2EPrerequisites,
): Promise<void> {
  await requireImage("docker");

  if (backend === CLOUD_HYPERVISOR_BACKEND) {
    await requirePath("/dev/kvm", "Cloud Hypervisor e2e requires /dev/kvm");
    if (e2eEnv.cloudHypervisorPath === undefined) {
      await requireCommand("cloud-hypervisor");
    }
    if (e2eEnv.passtPath === undefined) {
      await requireCommand("passt");
    }
    await requireCommand("virtiofsd");
  }

  if (
    prerequisites === DOCKER_HOST_PREREQUISITES ||
    prerequisites === KIND_READY_PREREQUISITES ||
    prerequisites === K3D_READY_PREREQUISITES
  ) {
    await requireCommand("docker");
  }
  if (prerequisites === KIND_READY_PREREQUISITES) {
    await requireCommand("kind");
    await requireCommand("kubectl");
  }
  if (prerequisites === K3D_READY_PREREQUISITES) {
    await requireCommand("k3d");
    await requireCommand("kubectl");
  }
}

function detectBackend(): string {
  return process.platform === "linux"
    ? CLOUD_HYPERVISOR_BACKEND
    : "virtualization-framework";
}

async function requireCommand(name: string): Promise<void> {
  const result = await create$({
    reject: false,
    timeout: 10_000,
  })`which ${name}`;
  if (result.exitCode !== 0) {
    throw new Error(`missing required command for e2e: ${name}`);
  }
}

async function requireImage(name: string): Promise<void> {
  await requirePath(
    path.join(imageStore(), name),
    `e2e requires image ${name} at ${path.join(imageStore(), name)}`,
  );
}

function imageStore(): string {
  return e2eEnv.imageStore;
}

async function requirePath(targetPath: string, message: string): Promise<void> {
  try {
    await access(targetPath);
  } catch {
    throw new Error(message);
  }
}

async function mkdtempInE2ERoot(): Promise<string> {
  const parent = e2eEnv.e2eRoot;
  await mkdir(parent, { recursive: true });
  return mkdtemp(path.join(parent, "run-"));
}

async function waitForTCPPortAttempt({
  deadline,
  host,
  port,
}: {
  readonly deadline: number;
  readonly host: string;
  readonly port: number;
}): Promise<string | undefined> {
  const lastError = await tryTCPConnect({ host, port, timeoutMs: 1000 });

  if (lastError === undefined) {
    return undefined;
  }
  if (Date.now() >= deadline) {
    return lastError;
  }

  await Bun.sleep(100);
  return waitForTCPPortAttempt({ deadline, host, port });
}

async function tryTCPConnect({
  host,
  port,
  timeoutMs,
}: {
  readonly host: string;
  readonly port: number;
  readonly timeoutMs: number;
}): Promise<string | undefined> {
  const socket = createConnection({ host, port });
  const abortController = new AbortController();
  const connectAttempt = waitForSocketConnect(socket, abortController.signal);
  const timeoutAttempt = waitForTimeout(timeoutMs, abortController.signal);

  try {
    return await Promise.race([connectAttempt, timeoutAttempt]);
  } finally {
    abortController.abort();
    socket.destroy();
  }
}

async function waitForSocketConnect(
  socket: Socket,
  signal: AbortSignal,
): Promise<string | undefined> {
  try {
    await once(socket, "connect", { signal });
    return undefined;
  } catch (error) {
    return errorMessageFrom(error);
  }
}

async function waitForTimeout(
  timeoutMs: number,
  signal: AbortSignal,
): Promise<string> {
  try {
    await delay(timeoutMs, undefined, { signal });
    return `timeout after ${timeoutMs.toString()}ms`;
  } catch (error) {
    return errorMessageFrom(error);
  }
}

function errorMessageFrom(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}
