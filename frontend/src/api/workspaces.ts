import { apiClient } from './client'

export interface Workspace {
  id: number
  name: string
  is_default: boolean
  role: 'admin' | 'member'
}

export interface WorkspaceMember {
  user_id: number
  role: 'admin' | 'member'
}

export const workspacesAPI = {
  async list(): Promise<Workspace[]> {
    return (await apiClient.get<Workspace[]>('/workspaces')).data
  },
  async create(name: string): Promise<Workspace> {
    return (await apiClient.post<Workspace>('/workspaces', { name })).data
  },
  async members(workspaceID: number): Promise<WorkspaceMember[]> {
    return (await apiClient.get<WorkspaceMember[]>(`/workspaces/${workspaceID}/members`)).data
  },
  async setMember(workspaceID: number, userID: number, role: WorkspaceMember['role']): Promise<void> {
    await apiClient.put(`/workspaces/${workspaceID}/members/${userID}`, { role })
  },
  async removeMember(workspaceID: number, userID: number): Promise<void> {
    await apiClient.delete(`/workspaces/${workspaceID}/members/${userID}`)
  },
}
