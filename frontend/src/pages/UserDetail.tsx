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

import {useEffect, useState} from 'react';
import {useTranslation} from 'react-i18next';
import {toast} from 'sonner';
import {ArrowLeft, Key, Trash2, UserPen} from 'lucide-react';
import {useNavigation} from '@/contexts/NavigationContext';
import {useAppStore} from '@/store';
import {StatusBadge} from '@/components/common/StatusBadge';
import {Container} from '@/components/common/Container';
import {FormField} from '@/components/common/FormField';
import {ResourceManager} from '@/components/resources/ResourceManager';
import {buttons, inputs} from '@/components/common/controls';
import {ConfirmModal} from '@/components/common/ConfirmModal';
import {cn} from '@/lib/utils';

export default function UserDetail() {
    const {t} = useTranslation();
    const {navigate} = useNavigation();
    const {users, servers, resources, selectedUserId, setSelectedUser, updateUser, deleteUser, refreshData, getInterfaceName} = useAppStore();
    const [isEditing, setIsEditing] = useState(false);
    const [isLoading, setIsLoading] = useState(false);
    const [showDeleteUserConfirm, setShowDeleteUserConfirm] = useState(false);
    const [deleteUserLoading, setDeleteUserLoading] = useState(false);
    const [peerServerId, setPeerServerId] = useState('');
    const [formData, setFormData] = useState({name: '', description: '', disabled: false});
    const user = users.find(item => item.id === selectedUserId);
    const peers = resources.filter(resource => resource.kind === 'Peer' && resource.spec.userRef === selectedUserId);

    useEffect(() => {
        if (!user) return;
        const timeout = window.setTimeout(() => setFormData({name: user.name || '', description: user.description || '', disabled: user.disabled || false}), 0);
        return () => window.clearTimeout(timeout);
    }, [user]);

    useEffect(() => {
        if (peerServerId || servers.length === 0) return;
        const timeout = window.setTimeout(() => setPeerServerId(peers[0]?.serverId ?? servers[0].id), 0);
        return () => window.clearTimeout(timeout);
    }, [peerServerId, peers, servers]);

    if (!user) return <div className="p-8 text-center text-muted-foreground">{t('users.noUser')}</div>;

    const handleSave = async () => {
        if (!selectedUserId || !formData.name.trim()) {
            toast.error(t('users.nameRequired'));
            return;
        }
        setIsLoading(true);
        try {
            const updated = await updateUser(selectedUserId, {name: formData.name, description: formData.description || undefined, disabled: formData.disabled});
            if (updated) {
                toast.success(t('users.userUpdated'));
                setIsEditing(false);
                await refreshData();
            } else {
                toast.error(t('users.updateUserError'));
            }
        } finally {
            setIsLoading(false);
        }
    };
    const handleBack = () => {
        setSelectedUser(null);
        navigate('users');
    };
    const handleDeleteUser = async () => {
        if (!selectedUserId) return;
        setDeleteUserLoading(true);
        try {
            if (await deleteUser(selectedUserId)) {
                toast.success(t('users.userDeleted'));
                handleBack();
            } else {
                toast.error(t('users.deleteUserError'));
            }
        } finally {
            setDeleteUserLoading(false);
            setShowDeleteUserConfirm(false);
        }
    };

    return (
        <div className="flex flex-col">
            <div className="flex items-center justify-between px-8 pt-6">
                <h2 className="font-semibold">{user.name || t('users.noName')}</h2>
                <div className="flex items-center gap-2">
                    <button onClick={() => setShowDeleteUserConfirm(true)} className={buttons.danger} title={t('users.deleteUser')}><Trash2 size={14} /></button>
                    <button onClick={handleBack} className={buttons.primary} title={t('users.backToUsers')}><ArrowLeft size={14} /></button>
                </div>
            </div>
            <div className="space-y-6 p-8">
                <Container title={t('users.information')} action={!isEditing && <button onClick={() => setIsEditing(true)} className={buttons.primary}><UserPen size={14} /></button>}>
                    {isEditing ? (
                        <form className="space-y-4" onSubmit={event => event.preventDefault()}>
                            <FormField label={t('common.name')}><input value={formData.name} onChange={event => setFormData(current => ({...current, name: event.target.value}))} disabled={isLoading} className={inputs.primary} /></FormField>
                            <FormField label={t('common.description')}><textarea value={formData.description} onChange={event => setFormData(current => ({...current, description: event.target.value}))} disabled={isLoading} rows={4} className={cn(inputs.primary, 'resize-none')} /></FormField>
                            <label className="flex items-center gap-3 text-sm"><input type="checkbox" checked={formData.disabled} onChange={event => setFormData(current => ({...current, disabled: event.target.checked}))} disabled={isLoading} /><span>{t('common.disabled')}</span></label>
                            <div className="flex gap-3 pt-4">
                                <button type="button" onClick={() => void handleSave()} disabled={isLoading} className={buttons.primary}>{isLoading ? `${t('common.saving')}...` : t('common.save')}</button>
                                <button type="button" onClick={() => setIsEditing(false)} disabled={isLoading} className={buttons.secondary}>{t('common.cancel')}</button>
                            </div>
                        </form>
                    ) : (
                        <div className="flex items-center gap-4 text-sm"><span className="flex-1 font-semibold">{user.description || '—'}</span><StatusBadge status={user.disabled ? 'disabled' : 'enabled'} size="sm" /></div>
                    )}
                </Container>
                <Container title={<span className="flex items-center gap-2 text-lg font-semibold"><Key size={20} />{t('users.peers')} ({peers.length})</span>}>
                    {peers.length === 0 ? <div className="py-4 text-center text-muted-foreground">{t('users.noPeers')}</div> : (
                        <div className="mb-5 space-y-2">
                            {peers.map(peer => {
                                const server = servers.find(item => item.id === peer.serverId);
                                return (
                                    <div key={peer.id} className="flex items-center justify-between gap-4 rounded-lg border border-border p-3 text-sm">
                                        <div className="min-w-0"><div className="truncate font-medium">{String(peer.spec.name ?? peer.name)}</div><div className="truncate text-xs text-muted-foreground">{server?.name ?? peer.serverId}/{getInterfaceName(String(peer.spec.interfaceRef ?? '')) ?? String(peer.spec.interfaceRef ?? '')}</div></div>
                                        <StatusBadge status={peer.spec.disabled === true ? 'disabled' : 'enabled'} size="sm" />
                                    </div>
                                );
                            })}
                        </div>
                    )}
                    {servers.length > 0 && (
                        <div className="border-t border-border pt-4">
                            <FormField label={t('peers.server')}>
                                <select value={peerServerId} onChange={event => setPeerServerId(event.target.value)} className={inputs.primary}>
                                    {servers.map(server => <option key={server.id} value={server.id}>{server.name}</option>)}
                                </select>
                            </FormField>
                            {peerServerId && <div className="mt-4"><ResourceManager serverId={peerServerId} kind="Peer" userId={user.id} onChanged={() => void refreshData()} /></div>}
                        </div>
                    )}
                </Container>
            </div>
            {showDeleteUserConfirm && <ConfirmModal title={t('users.deleteUser')} message={t('users.confirmDeleteUser')} onConfirm={() => void handleDeleteUser()} onCancel={() => setShowDeleteUserConfirm(false)} loading={deleteUserLoading} />}
        </div>
    );
}
