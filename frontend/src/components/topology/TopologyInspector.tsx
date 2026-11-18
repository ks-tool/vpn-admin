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

import {type FormEvent, useState} from 'react';
import {useTranslation} from 'react-i18next';
import {Plus, Trash2, X} from 'lucide-react';
import {toast} from 'sonner';
import {buttons, inputs} from '@/components/common/controls';
import {ConfirmModal} from '@/components/common/ConfirmModal';
import {FormField} from '@/components/common/FormField';
import {createNetworkInterface, deleteNetworkInterface, deleteTopologyServer, topologyCoordinates, updateNetworkInterface, updateTopologyServer} from '@/services/topology';
import type {NetworkInterface, Topology, TopologyServer} from '@/types';

export type TopologySelection = {kind: 'server' | 'interface'; id: string};

interface InspectorProps {
    selection: TopologySelection;
    topology: Topology;
    onClose: () => void;
    refresh: () => Promise<void>;
}

function message(error: unknown, fallback: string): string {
    return error instanceof Error ? error.message : fallback;
}

function InspectorShell({title, subtitle, onClose, children}: {title: string; subtitle?: string; onClose: () => void; children: React.ReactNode}) {
    const {t} = useTranslation();
    return (
        <aside className="flex h-full w-96 shrink-0 flex-col border-l border-border bg-card">
            <div className="flex items-start justify-between border-b border-border p-4">
                <div className="min-w-0"><h2 className="truncate font-semibold">{title}</h2>{subtitle && <p className="mt-1 truncate text-xs text-muted-foreground">{subtitle}</p>}</div>
                <button type="button" onClick={onClose} aria-label={t('common.close')} className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground"><X size={18} /></button>
            </div>
            <div className="flex-1 overflow-y-auto p-4">{children}</div>
        </aside>
    );
}

function ServerInspector({server, onClose, refresh}: {server: TopologyServer; onClose: () => void; refresh: () => Promise<void>}) {
    const {t} = useTranslation();
    const [name, setName] = useState(server.name);
    const [address, setAddress] = useState(server.address ?? '');
    const [description, setDescription] = useState(server.description ?? '');
    const [location, setLocation] = useState(server.location ?? '');
    const [latitude, setLatitude] = useState(server.latitude?.toString() ?? '');
    const [longitude, setLongitude] = useState(server.longitude?.toString() ?? '');
    const [interfaceName, setInterfaceName] = useState('');
    const [interfaceAddress, setInterfaceAddress] = useState('');
    const [busy, setBusy] = useState(false);
    const [confirmDelete, setConfirmDelete] = useState(false);

    const save = async (event: FormEvent) => {
        event.preventDefault();
        setBusy(true);
        try {
            await updateTopologyServer(server.id, {name, address, description, location, ...topologyCoordinates(latitude, longitude), interfaces: []});
            await refresh();
            toast.success(t('topology.saved'));
        } catch (error) {
            toast.error(message(error, t('topology.saveError')));
        } finally {
            setBusy(false);
        }
    };
    const addInterface = async (event: FormEvent) => {
        event.preventDefault();
        setBusy(true);
        try {
            await createNetworkInterface(server.id, {name: interfaceName, address: interfaceAddress});
            setInterfaceName('');
            setInterfaceAddress('');
            await refresh();
            toast.success(t('topology.interfaceCreated'));
        } catch (error) {
            toast.error(message(error, t('topology.saveError')));
        } finally {
            setBusy(false);
        }
    };
    const remove = async () => {
        setBusy(true);
        try {
            await deleteTopologyServer(server.id);
            setConfirmDelete(false);
            onClose();
            await refresh();
            toast.success(t('topology.deleted'));
        } catch (error) {
            toast.error(message(error, t('topology.deleteError')));
        } finally {
            setBusy(false);
        }
    };
    return (
        <InspectorShell title={server.name} subtitle={t('topology.server')} onClose={onClose}>
            <form onSubmit={save} className="space-y-4">
                <FormField label={t('topology.serverName')}><input value={name} onChange={event => setName(event.target.value)} required className={inputs.primary} /></FormField>
                <FormField label={t('topology.serverAddress')}><input value={address} onChange={event => setAddress(event.target.value)} className={inputs.primary} /></FormField>
                <FormField label={t('topology.descriptionField')}><textarea value={description} onChange={event => setDescription(event.target.value)} rows={3} className={inputs.primary} /></FormField>
                <FormField label={t('servers.location')}><input value={location} onChange={event => {setLocation(event.target.value); setLatitude(''); setLongitude('');}} placeholder={t('servers.locationPlaceholder')} className={inputs.primary} /></FormField>
                <div className="grid grid-cols-2 gap-3">
                    <FormField label={t('topology.latitude')}><input type="number" min={-90} max={90} step="any" value={latitude} onChange={event => setLatitude(event.target.value)} disabled={Boolean(location.trim())} className={inputs.primary} /></FormField>
                    <FormField label={t('topology.longitude')}><input type="number" min={-180} max={180} step="any" value={longitude} onChange={event => setLongitude(event.target.value)} disabled={Boolean(location.trim())} className={inputs.primary} /></FormField>
                </div>
                <button type="submit" disabled={busy} className={`${buttons.primary} w-full`}>{t('common.save')}</button>
            </form>
            <div className="mt-6 border-t border-border pt-4">
                <h3 className="mb-3 text-sm font-semibold">{t('topology.addInterface')}</h3>
                <form onSubmit={addInterface} className="space-y-3">
                    <input aria-label={t('topology.interfaceName')} placeholder="vpn0" value={interfaceName} onChange={event => setInterfaceName(event.target.value)} required className={inputs.primary} />
                    <input aria-label={t('topology.interfaceAddress')} placeholder="10.0.0.1/24" value={interfaceAddress} onChange={event => setInterfaceAddress(event.target.value)} className={inputs.primary} />
                    <button type="submit" disabled={busy} className={`${buttons.secondary} w-full`}><Plus size={14} />{t('topology.addInterface')}</button>
                </form>
            </div>
            <div className="mt-6 border-t border-border pt-4"><button type="button" onClick={() => setConfirmDelete(true)} className={`${buttons.danger} w-full`}><Trash2 size={14} />{t('topology.deleteServer')}</button></div>
            {confirmDelete && <ConfirmModal title={t('topology.deleteServer')} message={t('topology.confirmDeleteServer')} onCancel={() => setConfirmDelete(false)} onConfirm={() => void remove()} loading={busy} />}
        </InspectorShell>
    );
}

function InterfaceInspector({iface, topology, onClose, refresh}: {iface: NetworkInterface; topology: Topology; onClose: () => void; refresh: () => Promise<void>}) {
    const {t} = useTranslation();
    const name = iface.name;
    const [address, setAddress] = useState(iface.address ?? '');
    const [busy, setBusy] = useState(false);
    const [confirmDelete, setConfirmDelete] = useState(false);
    const server = topology.servers.find(item => item.id === iface.serverId);
    const save = async (event: FormEvent) => {
        event.preventDefault();
        setBusy(true);
        try {
            await updateNetworkInterface(iface.id, {name, address});
            await refresh();
            toast.success(t('topology.saved'));
        } catch (error) {
            toast.error(message(error, t('topology.saveError')));
        } finally {
            setBusy(false);
        }
    };
    const remove = async () => {
        setBusy(true);
        try {
            await deleteNetworkInterface(iface.id);
            setConfirmDelete(false);
            onClose();
            await refresh();
            toast.success(t('topology.deleted'));
        } catch (error) {
            toast.error(message(error, t('topology.deleteError')));
        } finally {
            setBusy(false);
        }
    };
    return (
        <InspectorShell title={iface.name} subtitle={server?.name} onClose={onClose}>
            <form onSubmit={save} className="space-y-4">
                <FormField label={t('topology.interfaceName')}><input value={name} readOnly disabled required className={inputs.primary} /></FormField>
                <FormField label={t('topology.interfaceAddress')}><input value={address} onChange={event => setAddress(event.target.value)} placeholder="10.0.0.1/24" className={inputs.primary} /></FormField>
                <button type="submit" disabled={busy} className={`${buttons.primary} w-full`}>{t('common.save')}</button>
            </form>
            <div className="mt-6 border-t border-border pt-4"><button type="button" onClick={() => setConfirmDelete(true)} className={`${buttons.danger} w-full`}><Trash2 size={14} />{t('topology.deleteInterface')}</button></div>
            {confirmDelete && <ConfirmModal title={t('topology.deleteInterface')} message={t('topology.confirmDeleteInterface')} onCancel={() => setConfirmDelete(false)} onConfirm={() => void remove()} loading={busy} />}
        </InspectorShell>
    );
}

export function TopologyInspector({selection, topology, onClose, refresh}: InspectorProps) {
    if (selection.kind === 'server') {
        const server = topology.servers.find(item => item.id === selection.id);
        if (server) return <ServerInspector key={`${server.id}:${server.latitude ?? ''}:${server.longitude ?? ''}`} server={server} onClose={onClose} refresh={refresh} />;
    }
    const iface = topology.interfaces.find(item => item.id === selection.id);
    if (iface) return <InterfaceInspector key={iface.id} iface={iface} topology={topology} onClose={onClose} refresh={refresh} />;
    return null;
}
