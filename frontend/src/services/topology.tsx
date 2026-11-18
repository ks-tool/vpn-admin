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
import {get, post, put, remove} from './api';
import {getCurrentApiMode} from './apiMode';
import type {NetworkInterface, NodePosition, Topology, TunnelMetric} from '@/types';

export interface TopologyServerInput {
    name: string;
    address: string;
    description: string;
    location: string;
    latitude: number | null;
    longitude: number | null;
    interfaces: NetworkInterfaceInput[];
}

export function topologyCoordinates(latitude: string, longitude: string): Pick<TopologyServerInput, 'latitude' | 'longitude'> {
    return {latitude: latitude.trim() === '' ? null : Number(latitude), longitude: longitude.trim() === '' ? null : Number(longitude)};
}

export interface NetworkInterfaceInput {
    name: string;
    address: string;
}

function errorMessage(error: unknown): string {
    if (error instanceof Error) return error.message;
    return typeof error === 'string' ? error : 'Unknown error';
}

async function binding<T>(operation: () => Promise<unknown>): Promise<T> {
    try {
        return await operation() as T;
    } catch (error) {
        throw new Error(errorMessage(error));
    }
}

function unwrap<T>(response: {data: T; error?: string}): T {
    if (response.error) throw new Error(response.error);
    return response.data;
}

const isDesktop = () => getCurrentApiMode() === 'bindings';

export async function getTopology(): Promise<Topology> {
    return isDesktop() ? binding(() => AppBindings.GetTopology()) : unwrap(await get<Topology>('/topology'));
}

export async function getTunnelMetrics(): Promise<TunnelMetric[]> {
    const result = isDesktop() ? await binding<TunnelMetric[]>(() => AppBindings.GetTunnelMetrics()) : unwrap(await get<TunnelMetric[]>('/topology/tunnel-metrics'));
    return result ?? [];
}

export async function updateTopologyServer(id: string, input: TopologyServerInput): Promise<Topology> {
    return isDesktop() ? binding(() => AppBindings.UpdateTopologyServer(id, input as never)) : unwrap(await put<Topology, TopologyServerInput>(`/topology/servers/${id}`, input));
}

export async function deleteTopologyServer(id: string): Promise<void> {
    if (isDesktop()) return binding(() => AppBindings.DeleteTopologyServer(id));
    unwrap(await remove<void>(`/topology/servers/${id}`));
}

export async function createNetworkInterface(serverId: string, input: NetworkInterfaceInput): Promise<Topology> {
    return isDesktop() ? binding(() => AppBindings.CreateNetworkInterface(serverId, input as never)) : unwrap(await post<Topology, NetworkInterfaceInput>(`/topology/servers/${serverId}/interfaces`, input));
}

export async function updateNetworkInterface(id: string, input: NetworkInterfaceInput): Promise<Topology> {
    return isDesktop() ? binding(() => AppBindings.UpdateNetworkInterface(id, input as never)) : unwrap(await put<Topology, NetworkInterfaceInput>(`/topology/interfaces/${id}`, input));
}

export async function deleteNetworkInterface(id: string): Promise<void> {
    if (isDesktop()) return binding(() => AppBindings.DeleteNetworkInterface(id));
    unwrap(await remove<void>(`/topology/interfaces/${id}`));
}

export async function moveTopologyNodes(positions: NodePosition[]): Promise<void> {
    if (isDesktop()) return binding(() => AppBindings.MoveTopologyNodes(positions as never));
    unwrap(await put<void, {positions: NodePosition[]}>('/topology/positions', {positions}));
}

export type {NetworkInterface};
