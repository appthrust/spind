import { expect, setDefaultTimeout, test } from "bun:test";
import { createE2EEnv, waitForTCPPort } from "./helpers";

setDefaultTimeout(45 * 60_000);

test("kind-ready snapshot restores a VM with Ready nodes", async () => {
  await using env = await createE2EEnv();
  const {
    $,
    baseVM,
    clusterName,
    context,
    kubeconfig,
    restoredKubeconfig,
    snapshot,
    workVM,
  } = env;

  // Create VM
  await $`spind vm create ${baseVM} --image docker`;

  // Start VM
  await $`spind vm start ${baseVM}`;

  // Create kind cluster
  await $`docker info`;
  await $`kind create cluster --name ${clusterName} --kubeconfig ${kubeconfig}`;

  const apiServerPort = apiServerPortFrom(
    await $`docker port ${clusterName}-control-plane 6443/tcp`,
  );

  await waitForTCPPort({
    host: "127.0.0.1",
    port: Number.parseInt(apiServerPort, 10),
    timeoutMs: 30_000,
  });
  await $`kubectl --kubeconfig ${kubeconfig} --context ${context} wait node --all --for=condition=Ready --timeout=180s`;

  // Create snapshot
  await $`spind snapshot create ${snapshot} --vm ${baseVM} --k8s=kind`;

  // Create VM from snapshot
  await $`spind vm create ${workVM} --snapshot ${snapshot}`;

  // Start VM from snapshot
  await $`spind vm start ${workVM}`;

  await $`kubectl --kubeconfig ${restoredKubeconfig} wait node --all --for=condition=Ready --timeout=180s`;

  const nodes = readyNodeNamesFrom(
    await $`kubectl --kubeconfig ${restoredKubeconfig} get nodes -o json`,
  );
  expect(nodes.length).toBeGreaterThan(0);
});

function apiServerPortFrom(apiServer: { stdout: string }): string {
  const port = apiServer.stdout.trim().split(":").at(-1);
  if (port === undefined) {
    throw new Error(
      `failed to resolve kind API server port: ${apiServer.stdout}`,
    );
  }
  expect(port).toMatch(/^\d+$/u);
  return port;
}

function readyNodeNamesFrom(result: { stdout: string }): Array<string> {
  const list = JSON.parse(result.stdout) as {
    items?: Array<{
      metadata?: { name?: string };
      status?: { conditions?: Array<{ type?: string; status?: string }> };
    }>;
  };
  const nodes = list.items ?? [];
  const ready = nodes.filter(
    (node) =>
      node.status?.conditions?.some(
        (condition) =>
          condition.type === "Ready" && condition.status === "True",
      ) ?? false,
  );
  if (nodes.length === 0 || ready.length !== nodes.length) {
    throw new Error(`not all nodes are Ready: ${result.stdout}`);
  }
  return ready.map((node) => node.metadata?.name ?? "<unknown>");
}
