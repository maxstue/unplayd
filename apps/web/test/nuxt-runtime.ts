// Unit tests provide the virtual Nitro runtime via vi.doMock("#imports", ...).
export function useRuntimeConfig(): never {
  throw new Error("Mock the Nitro runtime config in this unit test.");
}
