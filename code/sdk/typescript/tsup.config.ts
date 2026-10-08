// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Paul Fleischmann

import { defineConfig } from "tsup";

export default defineConfig({
  entry: ["src/index.ts"],
  format: ["esm", "cjs"],
  dts: true,
  sourcemap: true,
  clean: true,
  target: "node20",
});
