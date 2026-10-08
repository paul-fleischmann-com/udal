// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Paul Fleischmann

import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["src/**/*.test.ts"],
  },
});
