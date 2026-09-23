import type { PageResult } from './pagination'
import { api } from './api'
export interface Mapping {
  id: string; target_id: string; name: string; direction: 'local' | 'reverse'
  service_host: string; service_port: number; listen_port: number
  scope: 'loopback' | 'shared'; auto_start: boolean; revision: number
}
export interface MappingView extends Mapping {
  status: 'stopped' | 'connecting' | 'running' | 'error'
  error: string; last_error: string; warning: string; scope_verified: boolean
  connections: number; bind_address: string; access_addresses: string[]
}
export interface MappingSummary { total: number; running: number; stopped: number; error: number; by_target: Record<string, { total: number; running: number }> }
export interface MappingList extends PageResult<MappingView> { summary: MappingSummary; local_desktop: boolean; local_addresses: string[] }
export const fetchMappings = (query: URLSearchParams) => api<MappingList>(`/mappings?${query}`)
export const mappingPath = (id: string) => `/mappings/${encodeURIComponent(id)}`
