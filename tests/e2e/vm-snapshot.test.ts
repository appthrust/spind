import { expect, setDefaultTimeout, test } from "bun:test";
import { createE2EEnv } from "./helpers";

setDefaultTimeout(45 * 60_000);

test("VM snapshot can be created and inspected", async () => {
  await using env = await createE2EEnv("vm");
  const cleanup$ = env.cleanup$();
  const resources = vmSnapshotResources(env.id);

  try {
    await createBaseSnapshot(env, resources);
    await expectSnapshotFromStoppedVMToFail(env, resources);
    await createVMFromSnapshot(env, resources);
  } finally {
    await cleanup$`spind vm delete ${resources.restoredVM} --force`;
    await cleanup$`spind vm delete ${resources.vm} --force`;
    await cleanup$`spind snapshot delete ${resources.snapshot}-stopped`;
    await cleanup$`spind snapshot delete ${resources.snapshot}`;
  }
});

type Env = Awaited<ReturnType<typeof createE2EEnv>>;

type VMSnapshotResources = {
  readonly restoredVM: string;
  readonly snapshot: string;
  readonly vm: string;
};

function vmSnapshotResources(id: string): VMSnapshotResources {
  return {
    restoredVM: `e2e-vm-snapshot-restored-${id}`,
    snapshot: `e2e-vm-snapshot-prepared-${id}`,
    vm: `e2e-vm-snapshot-base-${id}`,
  };
}

async function createBaseSnapshot(
  env: Env,
  resources: VMSnapshotResources,
): Promise<void> {
  const $ = env.spind$;
  await $`spind vm create ${resources.vm} --image docker`;
  await $`spind vm start ${resources.vm}`;
  await $`spind vm exec ${resources.vm} -- uname`;
  await $`spind vm exec ${resources.vm} -- pwd`;
  await $`spind vm exec ${resources.vm} -- ls`;
  await $`spind vm exec ${resources.vm} -- sh -lc ${"echo hello > /tmp/spind-e2e.txt"}`;
  const marker =
    await $`spind vm exec ${resources.vm} -- cat /tmp/spind-e2e.txt`;
  expect(marker.stdout).toBe("hello");
  await $`spind vm exec ${resources.vm} -- ping -c 1 -W 3 127.0.0.1`;
  await $`spind vm list ${resources.vm}`;
  await $`spind snapshot create ${resources.snapshot} --vm ${resources.vm}`;
  await $`spind snapshot list`;
  await $`spind snapshot list --json`;
  await $`spind snapshot inspect ${resources.snapshot}`;
  await $`spind snapshot prune --all --dry-run`;
}

async function expectSnapshotFromStoppedVMToFail(
  env: Env,
  resources: VMSnapshotResources,
): Promise<void> {
  const cleanup$ = env.cleanup$();
  const result =
    await cleanup$`spind snapshot create ${resources.snapshot}-stopped --vm ${resources.vm}`;
  expect(result.exitCode).not.toBe(0);
}

async function createVMFromSnapshot(
  env: Env,
  resources: VMSnapshotResources,
): Promise<void> {
  const $ = env.spind$;
  await $`spind vm create ${resources.restoredVM} --snapshot ${resources.snapshot}`;
  await $`spind vm list ${resources.restoredVM}`;
  await $`spind vm list ${resources.restoredVM} --json`;
}
