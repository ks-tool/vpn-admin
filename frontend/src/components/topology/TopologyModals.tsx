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
import {Plus, Trash2} from 'lucide-react';
import {toast} from 'sonner';
import {Modal} from '@/components/common/Modal';
import {buttons, inputs} from '@/components/common/controls';
import {ServerFormFields} from '@/components/server/ServerFormFields';
import {type AuthType, formDataToServerInput, useServerForm} from '@/hooks/useServerForm';
import {createServer, generateAgentTLS} from '@/services/servers';
import {useAppStore} from '@/store';
import {createNetworkInterface, getTopology, topologyCoordinates, updateTopologyServer, type NetworkInterfaceInput} from '@/services/topology';
import type {Topology} from '@/types';

interface ServerModalProps {
    onClose: () => void;
    onCreated: (topology: Topology) => void;
}

export function AddServerModal({onClose, onCreated}: ServerModalProps) {
    const {t} = useTranslation();
    const refreshData = useAppStore(state => state.refreshData);
    const {formData, updateField} = useServerForm();
    const [authType, setAuthType] = useState<AuthType>('key');
    const [latitude, setLatitude] = useState('');
    const [longitude, setLongitude] = useState('');
    const [interfaces, setInterfaces] = useState<NetworkInterfaceInput[]>([{name: '', address: ''}]);
    const [saving, setSaving] = useState(false);
    const updateInterface = (index: number, field: keyof NetworkInterfaceInput, value: string) => setInterfaces(current => current.map((item, itemIndex) => itemIndex === index ? {...item, [field]: value} : item));
    const handleSubmit = async (event: FormEvent) => {
        event.preventDefault();
        setSaving(true);
        try {
            let server = await createServer(formDataToServerInput(formData, authType));
            if (!server) throw new Error(t('topology.saveError'));
            if (formData.agent.tlsEnabled) server = await generateAgentTLS(server.id) ?? server;
            await updateTopologyServer(server.id, {name: server.name, address: server.ssh.host, description: formData.info.description, location: formData.info.location, ...topologyCoordinates(latitude, longitude), interfaces: []});
            for (const iface of interfaces.filter(item => item.name.trim() || item.address.trim())) await createNetworkInterface(server.id, iface);
            await refreshData();
            toast.success(t('topology.serverCreated'));
            onCreated(await getTopology());
        } catch (error) {
            toast.error(error instanceof Error ? error.message : t('topology.saveError'));
        } finally {
            setSaving(false);
        }
    };
    return (
        <Modal title={t('topology.addServer')} onClose={onClose} loading={saving} size="lg">
            <form onSubmit={handleSubmit} className="space-y-4">
                <ServerFormFields formData={formData} onChange={updateField} disabled={saving} showAuth authType={authType} onAuthTypeChange={setAuthType} />
                <p className="text-xs text-muted-foreground">{t('topology.locationHint')}</p>
                <div className="grid gap-4 md:grid-cols-2">
                    <input aria-label={t('topology.latitude')} type="number" min={-90} max={90} step="any" value={latitude} onChange={event => setLatitude(event.target.value)} placeholder={t('topology.latitudePlaceholder')} disabled={saving || Boolean(formData.info.location.trim())} className={inputs.primary} />
                    <input aria-label={t('topology.longitude')} type="number" min={-180} max={180} step="any" value={longitude} onChange={event => setLongitude(event.target.value)} placeholder={t('topology.longitudePlaceholder')} disabled={saving || Boolean(formData.info.location.trim())} className={inputs.primary} />
                </div>
                <div>
                    <div className="mb-2 flex items-center justify-between"><label className="text-sm font-medium text-muted-foreground">{t('topology.interfaces')}</label><button type="button" onClick={() => setInterfaces(current => [...current, {name: '', address: ''}])} className={buttons.secondary}><Plus size={14} />{t('topology.addInterface')}</button></div>
                    <div className="space-y-2">{interfaces.map((iface, index) => <div key={index} className="flex gap-2"><input aria-label={t('topology.interfaceName')} value={iface.name} onChange={event => updateInterface(index, 'name', event.target.value)} placeholder="vpn0" className={inputs.primary} /><input aria-label={t('topology.interfaceAddress')} value={iface.address} onChange={event => updateInterface(index, 'address', event.target.value)} placeholder="10.0.0.1/24" className={inputs.primary} /><button type="button" aria-label={t('common.delete')} onClick={() => setInterfaces(current => current.filter((_, itemIndex) => itemIndex !== index))} className="shrink-0 rounded-lg px-3 text-muted-foreground hover:bg-red-500/10 hover:text-red-500"><Trash2 size={16} /></button></div>)}</div>
                </div>
                <div className="flex justify-end gap-2 pt-2"><button type="button" onClick={onClose} disabled={saving} className={buttons.secondary}>{t('common.cancel')}</button><button type="submit" disabled={saving} className={buttons.primary}>{saving ? t('common.saving') : t('common.create')}</button></div>
            </form>
        </Modal>
    );
}
