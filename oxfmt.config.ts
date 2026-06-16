import config from "@suin/oxlint/fmt/base";
import { defineConfig } from "oxfmt";

export default defineConfig({ ...config, ignorePatterns: ["docs/**/*.md"] });
