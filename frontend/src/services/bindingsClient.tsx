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

import * as AppBindings from '../../wailsjs/go/main/App';
import {models} from '../../wailsjs/go/models';

interface ApiResponse<T> {
    data: T;
    error?: string;
}

function extractErrorMessage(error: unknown): string {
    if (error instanceof Error) return error.message;
    if (typeof error === 'string') return error;
    return 'Unknown error';
}

async function response<T>(name: string, operation: () => Promise<T>, fallback?: T): Promise<ApiResponse<T>> {
    try {
        return {data: await operation()};
    } catch (error) {
        console.error(`Bindings ${name} failed:`, error);
        return {data: fallback as T, error: extractErrorMessage(error)};
    }
}

export const bindingsClient = {
    appVersion: () => response('AppVersion', () => AppBindings.AppVersion(), ''),
    listUsers: async () => response('ListUsers', async () => await AppBindings.ListUsers() || [], []),
    createUser: (input: unknown) => response('CreateUser', () => AppBindings.CreateUser(input as never)),
    updateUser: (id: string, input: unknown) => response('UpdateUser', () => AppBindings.UpdateUser(id, input as never)),
    deleteUser: (id: string) => response('DeleteUser', () => AppBindings.DeleteUser(id), undefined),
    listServers: async () => response('ListServers', async () => await AppBindings.ListServers() || [], []),
    updateServer: (id: string, input: unknown) => response('UpdateServer', () => AppBindings.UpdateServer(id, input as never)),
    syncServer: (id: string) => response('SyncServer', () => AppBindings.SyncServer(id), undefined),
    generateAgentTLS: (id: string) => response('GenerateAgentTLS', () => AppBindings.GenerateAgentTLS(id)),
    unlockServerSSH: (id: string, passphrase: string, applyToAll: boolean) => response('UnlockServerSSH', () => AppBindings.UnlockServerSSH(id, passphrase, applyToAll), undefined),
    getServerAgentStatus: (id: string) => response('ServerAgentStatus', () => AppBindings.ServerAgentStatus(id), ''),
    getServerHostInfo: (id: string) => response('ServerHostInfo', () => AppBindings.ServerHostInfo(id)),
    getServerMetricsHistory: (id: string) => response('GetServerMetricsHistory', () => AppBindings.GetServerMetricsHistory(id)),
    getServerInterfaceMetricsHistory: (serverId: string, interfaceId: string) => response('GetServerInterfaceMetricsHistory', () => AppBindings.GetServerInterfaceMetricsHistory(serverId, interfaceId)),
    getLogs: () => response('GetLogs', () => AppBindings.GetLogs(), ''),
    debugLoggingEnabled: () => response('DebugLoggingEnabled', () => AppBindings.DebugLoggingEnabled(), false),
    setDebugLogging: (enabled: boolean) => response('SetDebugLogging', () => AppBindings.SetDebugLogging(enabled), undefined),
    saveLogs: () => response('SaveLogs', () => AppBindings.SaveLogs(), false),
    saveBackup: () => response('SaveBackup', () => AppBindings.SaveBackup(), false),
    selectFile: (title: string) => response('SelectFile', () => AppBindings.SelectFile(title), ''),
};

export type {models};
