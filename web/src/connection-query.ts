// 独立窗口仅携带账号标识，凭证始终由服务端读取。
export function connectionQuery(target: string, separator = '?', shared?: string) {
  const params = new URLSearchParams(location.search)
  const selection = params.get('machine') === target ? params.get('connection') : ''
  const query = new URLSearchParams()
  if (selection) query.set('connection', selection)
  if (shared !== undefined) query.set('shared', shared)
  return query.size ? separator + query.toString() : ''
}
