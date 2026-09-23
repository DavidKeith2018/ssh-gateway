import type { InjectionKey } from 'vue'

// 同一机器页的终端、目录和资源请求共用一个错误弹窗。
export const connectionErrorsKey: InjectionKey<{
  report: (source: string, message: string) => void
  recover: (source: string) => void
}> = Symbol('connection-errors')
