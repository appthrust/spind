import config from "@suin/oxlint/lint/bun";
import { type OxlintConfig, defineConfig } from "oxlint";

const sharedConfig = config as unknown as OxlintConfig;

export default defineConfig({ extends: [sharedConfig] });
