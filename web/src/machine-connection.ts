import type { InjectionKey, ComputedRef } from 'vue'
export const machineConnectionKey: InjectionKey<ComputedRef<string>> = Symbol('machine-connection')
