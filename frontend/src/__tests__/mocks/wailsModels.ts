// Minimal mocks for wailsjs/go/models used in tests.
// Wails-generated model classes are just plain-object pass-through constructors.

class ModelClass {
  static createFrom(source: Record<string, unknown> = {}) {
    return new this(source);
  }

  constructor(init?: Record<string, unknown>) {
    if (init) Object.assign(this, init);
  }
}

export const llm_provider_svc = {
  CreateProviderRequest: ModelClass,
  UpdateProviderRequest: ModelClass,
  DeleteProviderRequest: ModelClass,
  TestConnectionRequest: ModelClass,
  ListModelsRequest: ModelClass,
  PreviewModelsRequest: ModelClass,
  LookupModelRequest: ModelClass,
};

export const agent_backend_svc = {
  CreateBackendRequest: ModelClass,
  UpdateBackendRequest: ModelClass,
  DeleteBackendRequest: ModelClass,
  TestBackendRequest: ModelClass,
  CancelTestBackendRequest: ModelClass,
  ResolveCLIPathRequest: ModelClass,
};

export const httpgateway = {};

export const chat_svc = {
  SendRequest: ModelClass,
};
