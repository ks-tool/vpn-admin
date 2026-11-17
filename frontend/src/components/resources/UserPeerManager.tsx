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

import {useEffect, useMemo, useState} from 'react';
import {useTranslation} from 'react-i18next';
import {ArrowRightLeft, FileInput, Key, Plus, Power, PowerOff, QrCode, Trash2} from 'lucide-react';
import {toast} from 'sonner';
import {ConfirmModal} from '@/components/common/ConfirmModal';
import {Container} from '@/components/common/Container';
import {FormField} from '@/components/common/FormField';
import {Modal} from '@/components/common/Modal';
import {buttons, inputs} from '@/components/common/controls';
import {StatusBadge} from '@/components/common/StatusBadge';
import {SchemaForm} from '@/components/topology/SchemaForm';
import {cn} from '@/lib/utils';
import {
    deleteProviderResource,
    getProviderSchema,
    putProviderResource,
    runProviderResourceAction,
} from '@/services/resources';
import {ResourceAction, type ActionState} from './ResourceManager';
import type {ActionResult, ProviderDefinition, ProviderResource, ResourceDefinition, Server, User} from '@/types';

interface UserPeerManagerProps {
    user: User;
    servers: Server[];
    resources: ProviderResource[];
    onChanged: () => Promise<void>;
}

interface PeerDetails {
    provider: string;
    definition: ResourceDefinition;
}

function providerName(resource: ProviderResource): string {
    return resource.apiVersion.split('.')[0];
}

function peerName(peer: ProviderResource): string {
    return typeof peer.spec.name === 'string' && peer.spec.name.length > 0 ? peer.spec.name : peer.name;
}

function clientEndpoint(server: Server | undefined, iface: ProviderResource | undefined): string {
    const host = server?.ssh.host.trim() ?? '';
    const listen = Number(iface?.spec.listen ?? 0);
    if (!host || !listen) return '';
    const unbracketed = host.startsWith('[') && host.endsWith(']') ? host.slice(1, -1) : host;
    return `${unbracketed.includes(':') ? `[${unbracketed}]` : unbracketed}:${listen}`;
}

export function UserPeerManager({user, servers, resources, onChanged}: UserPeerManagerProps) {
    const {t} = useTranslation();
    const peers = useMemo(() => resources.filter(resource => resource.kind === 'Peer' && resource.spec.userRef === user.id), [resources, user.id]);
    const interfaces = useMemo(() => resources.filter(resource => resource.kind === 'Interface'), [resources]);
    const [details, setDetails] = useState<Record<string, PeerDetails>>({});
    const [adding, setAdding] = useState(false);
    const [deleting, setDeleting] = useState<ProviderResource | null>(null);
    const [action, setAction] = useState<ActionState | null>(null);
    const [busy, setBusy] = useState(false);

    useEffect(() => {
        let cancelled = false;
        const unique = new Map<string, {serverId: string; provider: string}>();
        for (const peer of peers) {
            const provider = providerName(peer);
            unique.set(`${peer.serverId}/${provider}`, {serverId: peer.serverId, provider});
        }
        void Promise.all(Array.from(unique.values(), async item => ({
            ...item,
            schema: await getProviderSchema(item.serverId, item.provider),
        }))).then(entries => {
            if (cancelled) return;
            const schemas = new Map(entries.map(entry => [`${entry.serverId}/${entry.provider}`, entry.schema]));
            const next: Record<string, PeerDetails> = {};
            for (const peer of peers) {
                const provider = providerName(peer);
                const definition = schemas.get(`${peer.serverId}/${provider}`)?.resources.find(item => item.kind === 'Peer');
                if (definition) next[peer.id] = {provider, definition};
            }
            setDetails(next);
        }).catch(error => toast.error(error instanceof Error ? error.message : t('resources.loadError')));
        return () => {cancelled = true;};
    }, [peers, t]);

    const toggleDisabled = async (peer: ProviderResource) => {
        setBusy(true);
        try {
            const disabled = peer.spec.disabled === true;
            await putProviderResource(peer.serverId, providerName(peer), peer.kind, peer.name, {...peer.spec, disabled: !disabled});
            await onChanged();
            toast.success(t(disabled ? 'peers.activated' : 'peers.deactivated'));
        } catch (error) {
            toast.error(error instanceof Error ? error.message : t('peers.toggleError'));
        } finally {
            setBusy(false);
        }
    };

    const remove = async () => {
        if (!deleting) return;
        setBusy(true);
        try {
            await deleteProviderResource(deleting.serverId, providerName(deleting), deleting.kind, deleting.name);
            setDeleting(null);
            await onChanged();
            toast.success(t('peers.deleted'));
        } catch (error) {
            toast.error(error instanceof Error ? error.message : t('peers.deleteError'));
        } finally {
            setBusy(false);
        }
    };

    const executeAction = async (state: ActionState, args: Record<string, unknown>): Promise<ActionResult> => {
        setBusy(true);
        try {
            const result = await runProviderResourceAction(state.item.resource.serverId, state.item.provider, state.item.resource.kind, state.item.resource.name, state.action.name, args);
            if (state.action.mutating) await onChanged();
            return result;
        } finally {
            setBusy(false);
        }
    };

    const addPeer = async (form: PeerFormData) => {
        const selected = interfaces.find(item => item.id === form.interfaceId);
        if (!selected) throw new Error(t('peers.interfaceRequired'));
        const provider = providerName(selected);
        const name = form.name.trim();
        if (resources.some(item => item.serverId === selected.serverId && item.kind === 'Peer' && providerName(item) === provider && item.name === name)) {
            throw new Error(t('resources.saveError'));
        }
        setBusy(true);
        try {
            await putProviderResource(selected.serverId, provider, 'Peer', name, {
                ...form.spec,
                name,
                interfaceRef: selected.id,
                userRef: user.id,
                disabled: user.disabled,
            });
            setAdding(false);
            await onChanged();
            toast.success(t('peers.peerAdded'));
        } finally {
            setBusy(false);
        }
    };

    return (
        <Container
            title={<span className="flex items-center gap-2 text-lg font-semibold text-foreground dark:text-zinc-100"><Key size={20}/>{t('users.peers')} ({peers.length})</span>}
            action={<button onClick={() => setAdding(true)} className={buttons.primary} title={t('peers.addPeer')}><Plus size={14}/></button>}
        >
            {peers.length > 0 ? (
                <div className="space-y-3">
                    {peers.map(peer => {
                        const server = servers.find(item => item.id === peer.serverId);
                        const interfaceRef = String(peer.spec.interfaceRef ?? '');
                        const iface = interfaces.find(item => item.id === interfaceRef);
                        const disabled = peer.spec.disabled === true;
                        const detail = details[peer.id];
                        const pendingSecret = peer.status.conditions?.some(condition => condition.reason === 'SecretMigrationPending') === true || peer.status.provider?.secretMigrationPending === true;
                        const actions = detail?.definition.actions?.filter(item => item.name === 'renderClientConfig' && !pendingSecret || item.name === 'migratePeer' || item.name === 'importSecret' && pendingSecret) ?? [];
                        return (
                            <div key={peer.id} className="rounded-lg border border-input bg-background p-4 dark:border-white/10 dark:bg-white/5">
                                <div className="mb-2 flex items-center justify-between">
                                    <div className="flex min-w-0 items-center gap-2">
                                        <div className="min-w-0 truncate text-sm font-medium text-foreground dark:text-zinc-300">{peerName(peer)}</div>
                                        <span className="shrink-0"><StatusBadge status={disabled ? 'disabled' : 'enabled'} size="sm"/></span>
                                    </div>
                                    <div className="flex items-center gap-2">
                                        {detail && actions.map(actionDefinition => (
                                            <button
                                                key={actionDefinition.name}
                                                onClick={() => setAction({
                                                    item: {resource: peer, ...detail},
                                                    action: actionDefinition,
                                                    initialArguments: actionDefinition.name === 'renderClientConfig' ? {endpoint: clientEndpoint(server, iface)} : undefined,
                                                })}
                                                disabled={busy}
                                                className="rounded p-1 text-muted-foreground transition-colors hover:text-foreground dark:text-zinc-500 dark:hover:text-zinc-300"
                                                title={actionDefinition.title}
                                            >
                                                {actionDefinition.name === 'renderClientConfig' ? <QrCode size={16}/> : actionDefinition.name === 'migratePeer' ? <ArrowRightLeft size={16}/> : <FileInput size={16}/>}
                                            </button>
                                        ))}
                                        <button
                                            onClick={() => void toggleDisabled(peer)}
                                            disabled={busy}
                                            className={cn('rounded p-1 transition-colors disabled:cursor-not-allowed disabled:opacity-40', disabled ? 'text-muted-foreground hover:bg-emerald-100 hover:text-emerald-600 dark:hover:bg-emerald-500/10 dark:hover:text-emerald-400' : 'text-muted-foreground hover:bg-orange-100 hover:text-orange-600 dark:hover:bg-orange-500/10 dark:hover:text-orange-400')}
                                            title={disabled ? t('peers.activateTooltip') : t('peers.deactivateTooltip')}
                                        >
                                            {disabled ? <Power size={16}/> : <PowerOff size={16}/>}
                                        </button>
                                        <button
                                            onClick={() => setDeleting(peer)}
                                            disabled={busy}
                                            className="rounded p-1 text-muted-foreground transition-colors hover:bg-red-100 hover:text-red-600 dark:hover:bg-red-500/10 dark:hover:text-red-400"
                                            title={t('peers.deletePeerTooltip')}
                                        >
                                            <Trash2 size={16}/>
                                        </button>
                                    </div>
                                </div>
                                <div className="text-xs text-muted-foreground dark:text-zinc-500">
                                    <span className="font-semibold text-foreground dark:text-zinc-300">{t('peers.server')}: {server?.name ?? peer.serverId}/{iface?.name ?? interfaceRef}</span>
                                </div>
                            </div>
                        );
                    })}
                </div>
            ) : <div className="py-4 text-center text-muted-foreground dark:text-zinc-500">{t('users.noPeers')}</div>}
            {adding && <AddPeerModal servers={servers} interfaces={interfaces} busy={busy} onSubmit={addPeer} onClose={() => setAdding(false)}/>}
            {action && <ResourceAction state={action} busy={busy} onClose={() => setAction(null)} onExecute={executeAction}/>}
            {deleting && <ConfirmModal title={t('peers.deletePeerTooltip')} message={t('peers.confirmDelete')} onConfirm={() => void remove()} onCancel={() => setDeleting(null)} loading={busy}/>}
        </Container>
    );
}

interface PeerFormData {
    name: string;
    serverId: string;
    interfaceId: string;
    spec: Record<string, unknown>;
}

function AddPeerModal({servers, interfaces, busy, onSubmit, onClose}: {
    servers: Server[];
    interfaces: ProviderResource[];
    busy: boolean;
    onSubmit: (form: PeerFormData) => Promise<void>;
    onClose: () => void;
}) {
    const {t} = useTranslation();
    const [form, setForm] = useState<PeerFormData>({name: '', serverId: '', interfaceId: '', spec: {}});
    const [definition, setDefinition] = useState<ProviderDefinition | null>(null);
    const selected = interfaces.find(item => item.id === form.interfaceId && item.serverId === form.serverId);

    useEffect(() => {
        if (!selected) return;
        let cancelled = false;
        void getProviderSchema(selected.serverId, providerName(selected)).then(value => {
            if (!cancelled) setDefinition(value);
        }).catch(error => toast.error(error instanceof Error ? error.message : String(error)));
        return () => {cancelled = true;};
    }, [selected]);

    const peerSchema = useMemo(() => {
        const resource = definition?.resources.find(item => item.kind === 'Peer');
        if (!resource) return null;
        const internal = new Set(['name', 'interfaceRef', 'userRef', 'publicKey', 'disabled']);
        const properties = Object.fromEntries(Object.entries(resource.specSchema.properties ?? {}).filter(([key]) => !internal.has(key)));
        const required = Array.isArray(resource.specSchema.required) ? resource.specSchema.required.filter(key => typeof key === 'string' && !internal.has(key)) : [];
        return {...resource.specSchema, properties, required};
    }, [definition]);

    const submit = async (spec: Record<string, unknown>) => {
        try {
            await onSubmit({...form, spec});
        } catch (error) {
            toast.error(error instanceof Error ? error.message : t('peers.addPeerError'));
        }
    };

    return (
        <Modal title={t('peers.addPeer')} onClose={onClose} loading={busy}>
            <div className="space-y-4">
                <FormField label={`${t('common.name')} (${t('common.required')})`}>
                    <input autoFocus value={form.name} onChange={event => setForm(current => ({...current, name: event.target.value}))} placeholder={t('peers.namePlaceholder')} disabled={busy} className={inputs.primary}/>
                </FormField>
                <FormField label={`${t('peers.server')} (${t('common.required')})`}>
                    <select value={form.serverId} onChange={event => {setDefinition(null); setForm(current => ({...current, serverId: event.target.value, interfaceId: '', spec: {}}));}} disabled={busy} className={inputs.primary}>
                        <option value="">{t('peers.selectServer')}</option>
                        {servers.map(server => <option key={server.id} value={server.id}>{server.name}</option>)}
                    </select>
                </FormField>
                <FormField label={`${t('servers.interfaces.name')} (${t('common.required')})`}>
                    <select value={form.interfaceId} onChange={event => {setDefinition(null); setForm(current => ({...current, interfaceId: event.target.value, spec: {}}));}} disabled={busy || !form.serverId} className={inputs.primary}>
                        <option value="">{form.serverId ? t('peers.selectInterface') : t('peers.selectServerFirst')}</option>
                        {interfaces.filter(item => item.serverId === form.serverId).map(item => <option key={item.id} value={item.id}>{item.name} ({String(item.spec.addr ?? item.spec.address ?? '')})</option>)}
                    </select>
                </FormField>
                {selected && !peerSchema && <div className="py-4 text-center text-sm text-muted-foreground">{t('common.loading')}</div>}
                {selected && peerSchema && <SchemaForm schema={peerSchema} formData={form.spec} onChange={spec => setForm(current => ({...current, spec}))} onSubmit={spec => void submit(spec)} submitLabel={busy ? `${t('common.saving')}...` : t('peers.addPeer')} disabled={busy || !form.name.trim()}/>}
                <button onClick={onClose} disabled={busy} className={cn('w-full', buttons.secondary)}>{t('common.cancel')}</button>
            </div>
        </Modal>
    );
}
