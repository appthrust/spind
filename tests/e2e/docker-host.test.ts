import { expect, setDefaultTimeout, test } from "bun:test";
import { access, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { createE2EEnv, waitForTCPPort } from "./helpers";

setDefaultTimeout(45 * 60_000);

test("Docker host endpoint works", async () => {
  await using env = await createE2EEnv("docker-host");
  const $ = env.spind$;
  const cleanup$ = env.cleanup$();
  const vm = `e2e-docker-host-${env.id}`;

  try {
    await $`spind vm create ${vm} --image docker`;
    await $`spind vm start ${vm}`;
    await $`spind vm list ${vm}`;
    await checkDockerHost({ env, hostShare: true, vm });

    await $`spind vm stop ${vm}`;
    expect(
      await pathExists(path.join(env.spindHome, "vms", vm, "docker.sock")),
    ).toBe(false);
  } finally {
    await cleanup$`spind vm delete ${vm} --force`;
  }
});

test("Docker host snapshot restore keeps DNS usable", async () => {
  await using env = await createE2EEnv("docker-host");
  const { $, baseVM, snapshot, workVM } = env;

  await $`spind vm create ${baseVM} --image docker`;
  await $`spind vm start ${baseVM}`;

  const baseDocker = env.docker$(baseVM);
  await dockerPull(baseDocker, "busybox:latest");
  await $`spind vm exec ${baseVM} -- getent hosts example.com`;
  await baseDocker`docker run --rm busybox nslookup example.com`;

  await $`spind snapshot create ${snapshot} --vm ${baseVM}`;
  await $`spind vm create ${workVM} --snapshot ${snapshot}`;
  await $`spind vm start ${workVM}`;

  const restoredDocker = env.docker$(workVM);
  await $`spind vm exec ${workVM} -- getent hosts example.com`;
  await restoredDocker`docker run --rm busybox nslookup example.com`;
});

type Env = Awaited<ReturnType<typeof createE2EEnv>>;

async function checkDockerHost({
  env,
  hostShare,
  vm,
}: {
  readonly env: Env;
  readonly hostShare: boolean;
  readonly vm: string;
}): Promise<void> {
  const docker = env.docker$(vm);
  await docker`docker ps`;
  await dockerPull(docker, "busybox:latest");
  const echo = await docker`docker run --rm busybox echo hello world`;
  expect(echo.stdout).toBe("hello world");
  const stdin = await docker({
    input: "hello stdin\n",
  })`docker run -i --rm busybox cat`;
  expect(stdin.stdout).toBe("hello stdin");
  if (hostShare) {
    const marker = `.spind-docker-host-share-${env.id}`;
    await writeFile(marker, "spind-host-share\n");
    try {
      const hostFile =
        await docker`docker run --rm -v ${process.cwd()}:/work busybox cat /work/${marker}`;
      expect(hostFile.stdout).toBe("spind-host-share");
    } finally {
      await rm(marker, { force: true });
    }
  }

  await docker`docker run --rm busybox nslookup example.com`;
  await docker`docker run --rm busybox wget -qO- http://example.com`;
  await docker({ reject: false })`docker rm -f spind-e2e-http`;
  const httpCommand = String.raw`printf "spind-e2e-http\n" > /tmp/index.html && httpd -f -p 8080 -h /tmp`;
  await docker`docker run -d --name spind-e2e-http -p 127.0.0.1::8080 busybox sh -c ${httpCommand}`;
  try {
    const dockerPort = await docker`docker port spind-e2e-http 8080/tcp`;
    const port = publishedPortFrom(dockerPort);
    await waitForTCPPort({ host: "127.0.0.1", port, timeoutMs: 30_000 });
  } finally {
    await docker({ reject: false })`docker rm -f spind-e2e-http`;
  }
}

async function dockerPull(
  docker: ReturnType<Env["docker$"]>,
  image: string,
): Promise<void> {
  await dockerPullAttempt({ attempt: 1, attempts: 8, docker, image });
}

async function dockerPullAttempt({
  attempt,
  attempts,
  docker,
  image,
}: {
  readonly attempt: number;
  readonly attempts: number;
  readonly docker: ReturnType<Env["docker$"]>;
  readonly image: string;
}): Promise<void> {
  const result = await docker({ reject: false })`docker pull ${image}`;
  if (result.exitCode === 0) {
    return;
  }
  if (attempt === attempts) {
    throw new Error(`failed to pull ${image}: ${result.stderr}`);
  }
  await Bun.sleep(5000);
  await dockerPullAttempt({ attempt: attempt + 1, attempts, docker, image });
}

function publishedPortFrom(result: { readonly stdout: string }): number {
  const port = result.stdout.trim().split(":").at(-1);
  if (port === undefined) {
    throw new Error(`failed to resolve published port: ${result.stdout}`);
  }
  expect(port).toMatch(/^\d+$/u);
  return Number.parseInt(port, 10);
}

async function pathExists(targetPath: string): Promise<boolean> {
  try {
    await access(targetPath);
    return true;
  } catch {
    return false;
  }
}
