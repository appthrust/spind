import { expect, setDefaultTimeout, test } from "bun:test";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { createE2EEnv } from "./helpers";

setDefaultTimeout(45 * 60_000);

test("spind up provisions a project snapshot and starts the project VM", async () => {
  await using env = await createE2EEnv();
  const projectName = `e2e-up-${Date.now().toString(16)}`;
  const projectRoot = path.join(env.root, "project");
  const kubeconfig = path.join(env.spindHome, "vms", projectName, "kubeconfig");
  await mkdir(projectRoot, { recursive: true });
  await writeFile(
    path.join(projectRoot, "spind.yaml"),
    [
      `name: ${projectName}`,
      "image: docker",
      "k8s: kind",
      "setup:",
      yamlListItem(`kind create cluster --name ${projectName}`),
      yamlListItem(
        `api_server_port="$(docker port ${projectName}-control-plane 6443/tcp | awk -F: 'END { print $NF }')" && kubectl config set-cluster kind-${projectName} --server "https://127.0.0.1:$api_server_port"`,
      ),
      yamlListItem(
        `kubectl --context kind-${projectName} wait node --all --for=condition=Ready --timeout=180s`,
      ),
      "",
    ].join("\n"),
  );

  const $ = env.project$(projectRoot);
  const cleanup$ = env.cleanup$();
  try {
    await $`spind up`;

    const vmList = await $`spind vm list ${projectName} --json`;
    const vm = JSON.parse(vmList.stdout) as {
      name?: string;
      fromSnapshot?: boolean;
      sourceSnapshot?: string;
      kubernetes?: { ready?: boolean; kubeconfig?: string };
    };
    expect(vm.name).toBe(projectName);
    expect(vm.fromSnapshot).toBe(true);
    expect(vm.sourceSnapshot).toBe(`${projectName}-provisioned`);

    await $`kubectl --kubeconfig ${kubeconfig} wait node --all --for=condition=Ready --timeout=180s`;
  } finally {
    await cleanup$`spind vm delete ${projectName} --force`;
    await cleanup$`spind vm delete ${projectName}-provisioning --force`;
    await cleanup$`spind snapshot delete ${projectName}-provisioned`;
  }
});

function yamlListItem(value: string): string {
  return `  - ${JSON.stringify(value)}`;
}
