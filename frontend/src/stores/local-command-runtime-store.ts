export type LocalCommandRuntimeController = {
  stop: () => Promise<void>;
};

const controllers = new Map<string, LocalCommandRuntimeController>();

export const localCommandRuntimeStore = {
  register(
    terminalId: string,
    controller: LocalCommandRuntimeController,
  ): void {
    controllers.set(terminalId, controller);
  },

  unregister(
    terminalId: string,
    controller: LocalCommandRuntimeController,
  ): void {
    if (controllers.get(terminalId) === controller) {
      controllers.delete(terminalId);
    }
  },

  async stop(terminalId: string): Promise<boolean> {
    const controller = controllers.get(terminalId);
    if (!controller) return false;
    await controller.stop();
    return true;
  },

  resetForTesting(): void {
    controllers.clear();
  },
};
