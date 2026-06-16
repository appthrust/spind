import { homedir } from "node:os";
import path from "node:path";

const execaVerbose = parseExecaVerbose(process.env["EXECA_VERBOSE"]);

export const e2eEnv = {
  cloudHypervisorPath: process.env["SPIND_CLOUD_HYPERVISOR"],
  e2eRoot:
    process.env["SPIND_E2E_ROOT"] ?? path.join(homedir(), ".spind", "e2e"),
  execaVerbose,
  imageStore:
    process.env["SPIND_IMAGE_STORE"] ??
    path.join(homedir(), ".spind", "images"),
  passtPath: process.env["SPIND_PASST"],
  path: `${process.cwd()}/bin:${process.env["PATH"] ?? ""}`,
} as const;

function parseExecaVerbose(
  value: string | undefined,
): "none" | "short" | "full" {
  return value === "short" || value === "full" ? value : "none";
}
