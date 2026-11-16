/*
  Copyright © 2026 Alexey Shulutkov <github@shulutkov.ru>

  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

  	http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.
 */

import {create} from 'zustand';
import type {ProviderResource, Server, User} from '@/types';
import * as usersService from '@/services/users';
import * as serversService from '@/services/servers';
import {listResources} from '@/services/resources';
import type {ServerInput} from '@/services/servers';
import {SSHPassphraseRequiredError} from '@/services/sshErrors';
import {byName} from '@/lib/utils';

interface AppState {
    servers: Server[];
    users: User[];
    resources: ProviderResource[];
    selectedUserId: string | null;
    isLoadingUsers: boolean;
    setSelectedUser: (id: string | null) => void;
    refreshData: () => Promise<void>;
    fetchServers: () => Promise<void>;
    updateServer: (id: string, input: ServerInput) => Promise<Server | null>;
    fetchUsers: () => Promise<void>;
    createUser: (input: usersService.CreateUserInput) => Promise<User | null>;
    updateUser: (id: string, input: usersService.UpdateUserInput) => Promise<User | null>;
    deleteUser: (id: string) => Promise<boolean>;
    getInterfaceName: (interfaceId: string) => string | null;
}

async function listServerResources(server: Server): Promise<ProviderResource[]> {
    try {
        return await listResources(server.id);
    } catch (error) {
        console.error(`Failed to fetch resources for server ${server.id}:`, error);
        return [];
    }
}

export const useAppStore = create<AppState>((set, get) => ({
    servers: [],
    users: [],
    resources: [],
    selectedUserId: null,
    isLoadingUsers: false,
    setSelectedUser: id => set({selectedUserId: id}),
    refreshData: async () => {
        await Promise.all([get().fetchServers(), get().fetchUsers()]);
    },
    fetchServers: async () => {
        try {
            const servers = await serversService.listServers();
            if (!servers) return;
            servers.sort(byName(server => server.name));
            const resources = (await Promise.all(servers.map(listServerResources))).flat();
            set({servers, resources});
        } catch (error) {
            console.error('Failed to fetch servers:', error);
        }
    },
    updateServer: async (id, input) => {
        try {
            const server = await serversService.updateServer(id, input);
            if (server) {
                const servers = get().servers.map(item => item.id === id ? server : item).sort(byName(item => item.name));
                set({servers});
            }
            return server;
        } catch (error) {
            if (error instanceof SSHPassphraseRequiredError) throw error;
            console.error('Failed to update server:', error);
            return null;
        }
    },
    fetchUsers: async () => {
        set({isLoadingUsers: true});
        try {
            const users = await usersService.listUsers();
            if (users) set({users: users.sort(byName(user => user.name))});
        } catch (error) {
            console.error('Failed to fetch users:', error);
        } finally {
            set({isLoadingUsers: false});
        }
    },
    createUser: async input => {
        try {
            const user = await usersService.createUser(input);
            if (user) set({users: [...get().users, user].sort(byName(item => item.name))});
            return user;
        } catch (error) {
            console.error('Failed to create user:', error);
            return null;
        }
    },
    updateUser: async (id, input) => {
        try {
            const user = await usersService.updateUser(id, input);
            if (user) set({users: get().users.map(item => item.id === id ? user : item).sort(byName(item => item.name))});
            return user;
        } catch (error) {
            console.error('Failed to update user:', error);
            return null;
        }
    },
    deleteUser: async id => {
        try {
            const success = await usersService.deleteUser(id);
            if (success) {
                set({users: get().users.filter(user => user.id !== id)});
                await get().fetchServers();
            }
            return success;
        } catch (error) {
            console.error('Failed to delete user:', error);
            return false;
        }
    },
    getInterfaceName: interfaceId => {
        return get().resources.find(resource => resource.id === interfaceId && resource.kind === 'Interface')?.name ?? null;
    },
}));
