import { afterEach, describe, expect, it, vi } from "vitest";

import {
  localCommandRuntimeStore,
  type LocalCommandRuntimeController,
} from "../local-command-runtime-store";

afterEach(() => {
  localCommandRuntimeStore.resetForTesting();
});

describe("local command runtime controller ownership", () => {
  it("Given a replacement controller generation, when the stale generation unregisters, then Stop still delegates only to the replacement", async () => {
    const stale: LocalCommandRuntimeController = {
      stop: vi.fn().mockResolvedValue(undefined),
    };
    const replacement: LocalCommandRuntimeController = {
      stop: vi.fn().mockResolvedValue(undefined),
    };

    localCommandRuntimeStore.register("terminal-1", stale);
    localCommandRuntimeStore.register("terminal-1", replacement);
    localCommandRuntimeStore.unregister("terminal-1", stale);

    await expect(localCommandRuntimeStore.stop("terminal-1")).resolves.toBe(
      true,
    );
    expect(stale.stop).not.toHaveBeenCalled();
    expect(replacement.stop).toHaveBeenCalledTimes(1);

    localCommandRuntimeStore.unregister("terminal-1", replacement);
    await expect(localCommandRuntimeStore.stop("terminal-1")).resolves.toBe(
      false,
    );
    expect(replacement.stop).toHaveBeenCalledTimes(1);
  });
});
