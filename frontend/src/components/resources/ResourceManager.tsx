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

import {useCallback, useEffect, useMemo, useState} from 'react';
import {useTranslation} from 'react-i18next';
import {Edit2, FileInput, Network, Play, Plus, Power, PowerOff, QrCode, RefreshCw, RotateCcw, ShieldOff, Trash2} from 'lucide-react';
import {toast} from 'sonner';
import {ConfirmModal} from '@/components/common/ConfirmModal';
import {FormField} from '@/components/common/FormField';
import {Modal} from '@/components/common/Modal';
import {buttons, inputs} from '@/components/common/controls';
import {SchemaForm} from '@/components/topology/SchemaForm';
import {logError} from '@/services/errorReporting';
import {syncServer} from '@/services/servers';
import {
    deleteProviderResource,
    getProviderManifest,
    getProviderSchema,
    listProviderResources,
    putProviderResource,
    runProviderResourceAction,
    saveActionResult,
} from '@/services/resources';
import {cn} from '@/lib/utils';
import {ClientConfigAction} from './ClientConfigAction';
import type {ActionDefinition, ActionResult, ProviderDefinition, ProviderManifest, ProviderResource, ResourceDefinition} from '@/types';

export interface ListedResource {
    provider: string;
    definition: ResourceDefinition;
    resource: ProviderResource;
}

interface EditorState {
    provider: string;
    kind: string;
    resource?: ProviderResource;
}

export interface ActionState {
    item: ListedResource;
    action: ActionDefinition;
    initialArguments?: Record<string, unknown>;
}

interface ResourceManagerProps {
    serverId: string;
    kind?: string;
    userId?: string;
    onChanged?: () => void;
}

export function ResourceManager({serverId, kind = 'Interface', userId, onChanged}: ResourceManagerProps) {
    const {t} = useTranslation();
    const [manifest, setManifest] = useState<ProviderManifest | null>(null);
    const [schemas, setSchemas] = useState<Record<string, ProviderDefinition>>({});
    const [resources, setResources] = useState<ListedResource[]>([]);
    const [loading, setLoading] = useState(true);
    const [syncing, setSyncing] = useState(false);
    const [editor, setEditor] = useState<EditorState | null>(null);
    const [deleting, setDeleting] = useState<ListedResource | null>(null);
    const [action, setAction] = useState<ActionState | null>(null);
    const [busy, setBusy] = useState(false);

    const load = useCallback(async () => {
        const nextManifest = await getProviderManifest(serverId);
        const definitions = await Promise.all(nextManifest.providers.map(provider => getProviderSchema(serverId, provider.name)));
        const nextSchemas = Object.fromEntries(definitions.map(definition => [definition.name, definition]));
        const lists = await Promise.all(nextManifest.providers.map(async provider => {
            const items = await listProviderResources(serverId, provider.name);
            return items.flatMap(resource => {
                if (resource.kind !== kind || userId && resource.spec.userRef !== userId) return [];
                const definition = nextSchemas[provider.name]?.resources.find(item => item.kind === resource.kind);
                return definition ? [{provider: provider.name, definition, resource}] : [];
            });
        }));
        setManifest(nextManifest);
        setSchemas(nextSchemas);
        setResources(lists.flat());
    }, [kind, serverId, userId]);

    const refresh = useCallback(async () => {
        try {
            await load();
        } catch (error) {
            toast.error(error instanceof Error ? error.message : t('resources.loadError'));
        } finally {
            setLoading(false);
        }
    }, [load, t]);

    useEffect(() => {
        const timeout = window.setTimeout(() => void refresh(), 0);
        return () => window.clearTimeout(timeout);
    }, [refresh]);

    const sync = useCallback(async () => {
        setSyncing(true);
        try {
            if (!await syncServer(serverId)) {
                toast.error(t('servers.syncError'));
                return;
            }
            await refresh();
            onChanged?.();
            toast.success(t('servers.syncSuccess'));
        } catch (error) {
            toast.error(error instanceof Error ? error.message : t('servers.syncError'));
        } finally {
            setSyncing(false);
        }
    }, [onChanged, refresh, serverId, t]);

    const save = useCallback(async (state: EditorState, name: string, spec: Record<string, unknown>) => {
        setBusy(true);
        try {
            if (userId) spec = {...spec, userRef: userId};
            await putProviderResource(serverId, state.provider, state.kind, name, spec);
            setEditor(null);
            await load();
            onChanged?.();
            toast.success(t('resources.saved'));
            window.setTimeout(() => void load().catch(() => undefined), 750);
        } catch (error) {
            toast.error(error instanceof Error ? error.message : t('resources.saveError'));
        } finally {
            setBusy(false);
        }
    }, [load, onChanged, serverId, userId, t]);

    const toggleDisabled = useCallback(async (item: ListedResource) => {
        setBusy(true);
        try {
            const disabled = item.resource.spec.disabled === true;
            await putProviderResource(serverId, item.provider, item.resource.kind, item.resource.name, {...item.resource.spec, disabled: !disabled});
            await load();
            onChanged?.();
            window.setTimeout(() => void load().catch(() => undefined), 750);
        } catch (error) {
            toast.error(error instanceof Error ? error.message : t('resources.saveError'));
        } finally {
            setBusy(false);
        }
    }, [load, onChanged, serverId, t]);

    const remove = useCallback(async () => {
        if (!deleting) return;
        setBusy(true);
        try {
            await deleteProviderResource(serverId, deleting.provider, deleting.resource.kind, deleting.resource.name);
            setDeleting(null);
            await load();
            onChanged?.();
            toast.success(t('resources.deleted'));
        } catch (error) {
            toast.error(error instanceof Error ? error.message : t('resources.deleteError'));
        } finally {
            setBusy(false);
        }
    }, [deleting, load, onChanged, serverId, t]);

    const executeAction = useCallback(async (state: ActionState, args: Record<string, unknown>) => {
        setBusy(true);
        try {
            const result = await runProviderResourceAction(serverId, state.item.provider, state.item.resource.kind, state.item.resource.name, state.action.name, args);
            if (state.action.mutating) {
                await load();
                onChanged?.();
                window.setTimeout(() => void load().catch(() => undefined), 750);
            }
            return result;
        } finally {
            setBusy(false);
        }
    }, [load, onChanged, serverId]);

    const providers = manifest?.providers.filter(provider => provider.resources.some(resource => resource.kind === kind)) ?? [];
    return (
        <div className="rounded-xl border border-border bg-card p-6 dark:border-white/5 dark:bg-white/3">
            <div className="mb-4 flex items-center justify-between">
                <h2 className="flex items-center gap-2 text-lg font-semibold"><Network size={20} />{t(kind === 'Peer' ? 'users.peers' : 'servers.interface')} ({resources.length})</h2>
                <div className="flex gap-2">
                    <button type="button" onClick={() => void sync()} disabled={syncing || busy || loading} className={cn(buttons.secondary, 'inline-flex items-center gap-1.5')} title={t('servers.syncTooltip')}><RefreshCw size={14} className={syncing ? 'animate-spin' : ''} /></button>
                    <button type="button" onClick={() => setEditor({provider: '', kind})} disabled={providers.length === 0} className={buttons.primary} title={t('resources.add')}><Plus size={14} /></button>
                </div>
            </div>
            {loading ? <div className="py-8 text-center text-sm text-muted-foreground">{t('common.loading')}</div> : resources.length === 0 ? (
                <div className="py-4 text-center text-muted-foreground">{t('resources.empty')}</div>
            ) : (
                <div className="space-y-3">
                    {resources.map(item => {
                        const condition = item.resource.status.conditions?.[0];
                        const disabled = item.resource.spec.disabled === true;
                        return (
                            <div key={`${item.provider}:${item.resource.kind}:${item.resource.name}`} className="rounded-lg border border-input bg-background p-4 dark:border-white/10 dark:bg-white/5">
                                <div className="flex items-start justify-between gap-4">
                                    <div className="min-w-0">
                                        <div className="flex flex-wrap items-center gap-2 text-sm font-semibold">
                                            <span className={cn('h-2 w-2 rounded-full', item.resource.status.phase === 'Ready' ? 'bg-emerald-500' : item.resource.status.phase === 'Degraded' ? 'bg-red-500' : 'bg-amber-500')} />
                                            {resourceDisplayName(item.resource)}
                                            <span className="rounded bg-muted px-1.5 py-0.5 text-xs font-normal text-muted-foreground">{item.provider}/{item.resource.kind}</span>
                                        </div>
                                        <div className="mt-1 text-xs text-muted-foreground">{item.resource.status.phase}</div>
                                        {condition?.message && <div className={cn('mt-2 text-xs', condition.status === 'False' ? 'text-red-500' : 'text-muted-foreground')}>{condition.reason}: {condition.message}</div>}
                                    </div>
                                    <div className="flex shrink-0 gap-2">
                                        {item.definition.actions?.map(actionDefinition => <button key={actionDefinition.name} type="button" disabled={busy} onClick={() => setAction({item, action: actionDefinition})} title={actionDefinition.title} aria-label={actionDefinition.title} className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"><ActionIcon name={actionDefinition.name} /></button>)}
                                        {supportsDisabled(item.definition) && <button type="button" disabled={busy} onClick={() => void toggleDisabled(item)} title={disabled ? t('resources.activate') : t('resources.deactivate')} className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground">{disabled ? <Power size={16} /> : <PowerOff size={16} />}</button>}
                                        <button type="button" disabled={busy} onClick={() => setEditor({provider: item.provider, kind: item.resource.kind, resource: item.resource})} title={t('common.edit')} className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"><Edit2 size={16} /></button>
                                        <button type="button" disabled={busy} onClick={() => setDeleting(item)} title={t('common.delete')} className="rounded p-1 text-muted-foreground hover:bg-red-100 hover:text-red-600 dark:hover:bg-red-500/10"><Trash2 size={16} /></button>
                                    </div>
                                </div>
                            </div>
                        );
                    })}
                </div>
            )}
            {editor && (!editor.provider || schemas[editor.provider]) && <ResourceEditor key={`${editor.provider}:${editor.kind}:${editor.resource?.id ?? 'new'}`} state={editor} manifest={manifest} schemas={schemas} userId={userId} busy={busy} onStateChange={setEditor} onClose={() => setEditor(null)} onSave={(name, spec) => void save(editor, name, spec)} />}
            {action && <ResourceAction state={action} busy={busy} onClose={() => setAction(null)} onExecute={executeAction} />}
            {deleting && <ConfirmModal title={t('resources.deleteTitle')} message={t('resources.deleteConfirm', {name: resourceDisplayName(deleting.resource)})} onCancel={() => setDeleting(null)} onConfirm={() => void remove()} loading={busy} />}
        </div>
    );
}

function ActionIcon({name}: {name: string}) {
    switch (name) {
        case 'importSecret':
            return <FileInput size={15} />;
        case 'renderClientConfig':
            return <QrCode size={16} />;
        case 'rotateKeys':
            return <RefreshCw size={15} />;
        case 'revoke':
            return <ShieldOff size={15} />;
        case 'restart':
            return <RotateCcw size={15} />;
        default:
            return <Play size={15} />;
    }
}

function resourceDisplayName(resource: ProviderResource): string {
    return typeof resource.spec.name === 'string' && resource.spec.name.length > 0 ? resource.spec.name : resource.name;
}

function supportsDisabled(definition: ResourceDefinition): boolean {
    const properties = definition.specSchema.properties as Record<string, {type?: string}> | undefined;
    return properties?.disabled?.type === 'boolean';
}

interface ResourceActionProps {
    state: ActionState;
    busy: boolean;
    onClose: () => void;
    onExecute: (state: ActionState, args: Record<string, unknown>) => Promise<ActionResult>;
}

export function ResourceAction(props: ResourceActionProps) {
    if (props.state.action.name === 'renderClientConfig') {
        return <ClientConfigAction state={props.state} onClose={props.onClose}/>;
    }
    return <GenericResourceAction {...props}/>;
}

function GenericResourceAction({state, busy, onClose, onExecute}: ResourceActionProps) {
    const {t} = useTranslation();
    const [formData, setFormData] = useState<Record<string, unknown>>(state.initialArguments ?? {});
    const [result, setResult] = useState<ActionResult | null>(null);
    const [error, setError] = useState<string | null>(null);
    const schema = state.action.argumentsSchema ?? {type: 'object', properties: {}};
    const execute = async (args: Record<string, unknown>) => {
        setError(null);
        try {
            setResult(await onExecute(state, args));
        } catch (cause) {
            const message = cause instanceof Error ? cause.message : t('resources.actionError');
            setError(message);
            logError(state.action.title, message);
        }
    };
    const resultText = result?.type === 'file' && typeof result.data === 'string' ? result.data : result ? JSON.stringify(result.data ?? {taskId: result.taskId}, null, 2) : '';
    return (
        <Modal title={state.action.title} onClose={onClose} loading={busy} size="lg">
            <div className="mb-3 text-sm text-muted-foreground">{resourceDisplayName(state.item.resource)} · {state.item.provider}/{state.item.resource.kind}</div>
            {state.action.description && <p className="mb-4 text-sm text-muted-foreground">{state.action.description}</p>}
            {state.action.requiresConfirmation && !result && <p className="mb-4 rounded-lg bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-300">{t('resources.actionConfirm')}</p>}
            {error && <p className="mb-4 rounded-lg bg-red-500/10 p-3 text-sm text-red-600 dark:text-red-400">{error}</p>}
            {result ? (
                <div>
                    <pre className="max-h-96 overflow-auto rounded-lg bg-muted p-4 text-xs whitespace-pre-wrap">{resultText}</pre>
                    <div className="mt-4 flex justify-end gap-2">
                        {result.type === 'file' && <button type="button" onClick={() => void saveActionResult(result, resultText)} className={buttons.secondary}>{t('resources.download')}</button>}
                        <button type="button" onClick={onClose} className={buttons.primary}>{t('common.close')}</button>
                    </div>
                </div>
            ) : <SchemaForm schema={schema} uiSchema={state.action.uiSchema} formData={formData} onChange={setFormData} onSubmit={execute} submitLabel={busy ? t('common.saving') : state.action.title} disabled={busy} />}
        </Modal>
    );
}

function ResourceEditor({state, manifest, schemas, userId, busy, onStateChange, onClose, onSave}: {
    state: EditorState;
    manifest: ProviderManifest | null;
    schemas: Record<string, ProviderDefinition>;
    userId?: string;
    busy: boolean;
    onStateChange: (state: EditorState) => void;
    onClose: () => void;
    onSave: (name: string, spec: Record<string, unknown>) => void;
}) {
    const {t} = useTranslation();
    const [step, setStep] = useState<'provider' | 'settings'>(state.resource ? 'settings' : 'provider');
    const [name, setName] = useState(state.resource?.name ?? '');
    const [formData, setFormData] = useState<Record<string, unknown>>({...state.resource?.spec, ...(userId ? {userRef: userId} : {})});
    const providers = useMemo(() => manifest?.providers.filter(provider => provider.resources.some(resource => resource.kind === state.kind)) ?? [], [manifest, state.kind]);
    const definition = useMemo(() => schemas[state.provider]?.resources.find(item => item.kind === state.kind), [schemas, state.provider, state.kind]);

    if (step === 'provider') {
        return (
            <Modal title={t('resources.add')} onClose={onClose} loading={busy} size="lg">
                <FormField label={t('resources.provider')}>
                    <select autoFocus value={state.provider} disabled={busy} onChange={event => onStateChange({provider: event.target.value, kind: state.kind})} className={inputs.primary}>
                        <option value="">{t('resources.selectProvider')}</option>
                        {providers.map(provider => <option key={provider.name} value={provider.name}>{provider.name} ({provider.version})</option>)}
                    </select>
                </FormField>
                <div className="mt-6 flex justify-end gap-2">
                    <button type="button" onClick={onClose} disabled={busy} className={buttons.secondary}>{t('common.cancel')}</button>
                    <button type="button" onClick={() => setStep('settings')} disabled={busy || !state.provider} className={buttons.primary}>{t('common.next')}</button>
                </div>
            </Modal>
        );
    }
    if (!definition) return null;
    const uiSchema = {
        ...definition.uiSchema,
        ...(state.resource?.kind === 'Interface' ? {iface: {'ui:readOnly': true}, name: {'ui:readOnly': true}} : {}),
        ...(userId ? {userRef: {'ui:widget': 'hidden'}} : {}),
    };
    return (
        <Modal title={state.resource ? t('resources.edit') : t('resources.add')} onClose={onClose} loading={busy} size="lg">
            <div className="mb-4 rounded-lg bg-muted px-3 py-2 text-sm"><span className="text-muted-foreground">{t('resources.provider')}: </span><span className="font-semibold">{state.provider}</span><span className="ml-2 font-mono text-xs text-muted-foreground">{definition.apiVersion}</span></div>
            {!state.resource && <button type="button" onClick={() => setStep('provider')} disabled={busy} className={cn(buttons.secondary, 'mb-4')}>{t('common.back')}</button>}
            <div className="mb-4"><FormField label={t('resources.name')}><input required value={name} disabled={busy || Boolean(state.resource)} onChange={event => setName(event.target.value)} className={inputs.primary} /></FormField></div>
            <SchemaForm schema={definition.specSchema} uiSchema={uiSchema} formData={formData} onChange={setFormData} onSubmit={spec => onSave(name.trim(), spec)} submitLabel={busy ? t('common.saving') : t('common.save')} disabled={busy || !name.trim()} />
        </Modal>
    );
}
