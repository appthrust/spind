import { expect, setDefaultTimeout, test } from "bun:test";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { K3D_READY_PREREQUISITES, createE2EEnv } from "./helpers";

setDefaultTimeout(45 * 60_000);

test("k3d registry snapshot restores a pullable registry", async () => {
  await using env = await createE2EEnv(K3D_READY_PREREQUISITES);
  const projectName = `e2e-k3d-${env.id}`;
  const registryName = `${projectName}-registry`;
  const imageName = "spind-e2e";
  const imageTag = "dev";
  const projectRoot = path.join(env.root, "project");
  await mkdir(projectRoot, { recursive: true });
  await writeFile(
    path.join(projectRoot, "spind.yaml"),
    [
      `name: ${projectName}`,
      "image: docker",
      "k8s: k3d",
      "setup:",
      yamlListItem(
        `k3d cluster create ${projectName} --registry-create ${registryName} --kubeconfig-update-default=false --timeout 180s`,
      ),
      yamlListItem(`k3d kubeconfig get ${projectName} > "$KUBECONFIG"`),
      yamlListItem(
        `kubectl --context k3d-${projectName} wait node --all --for=condition=Ready --timeout=180s`,
      ),
      yamlListItem(
        `registry_port="$(docker port ${registryName} 5000/tcp | awk -F: 'END { print $NF }')" && docker pull busybox:latest && docker tag busybox:latest "localhost:$registry_port/${imageName}:${imageTag}" && docker push "localhost:$registry_port/${imageName}:${imageTag}"`,
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
      dockerEndpointUri?: string;
      kubernetes?: { ready?: string; kubeconfig?: string };
      registry?: { ready?: boolean; url?: string };
    };
    expect(typeof vm.dockerEndpointUri).toBe("string");
    expect(vm.registry?.ready).toBe(true);
    expect(vm.registry?.url).toMatch(/^localhost:\d+$/u);

    const registryURL = vm.registry?.url;
    if (registryURL === undefined) {
      throw new Error(
        `missing registry or Docker endpoint in VM info: ${vmList.stdout}`,
      );
    }

    const docker$ = env.docker$(projectName);
    await docker$`docker pull ${registryURL}/${imageName}:${imageTag}`;

    const hosting =
      await $`kubectl --kubeconfig ${path.join(env.spindHome, "vms", projectName, "kubeconfig")} --namespace kube-public get configmap local-registry-hosting -o jsonpath={.data.localRegistryHosting\\.v1}`;
    expect(hosting.stdout).toContain(`host: ${registryURL}`);
  } finally {
    await cleanup$`spind vm delete ${projectName} --force`;
    await cleanup$`spind vm delete ${projectName}-provisioning --force`;
    await cleanup$`spind snapshot delete ${projectName}-provisioned`;
  }
});

function yamlListItem(value: string): string {
  return `  - ${JSON.stringify(value)}`;
}
