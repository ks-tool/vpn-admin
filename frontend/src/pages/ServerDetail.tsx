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
import {useAppStore} from '@/store';
import {unlockServerSSH} from '@/services/servers';
import {SSHPassphraseRequiredError} from '@/services/sshErrors';
import {SSHPassphraseModal} from '@/components/server/SSHPassphraseModal';
import {Container} from '@/components/common/Container';
import {ResourceManager} from '@/components/resources/ResourceManager';
import {ServerFormFields} from '@/components/server/ServerFormFields';
import {buttons} from '@/components/common/controls';
import {authTypeForServer, formDataToServerInput, serverToFormData, useServerForm, type AuthType} from '@/hooks/useServerForm';

interface ServerDetailProps {
    serverId: string;
    section: 'information' | 'interfaces';
    onChanged: () => void;
}

export default function ServerDetail({serverId, section, onChanged}: ServerDetailProps) {
    const {t} = useTranslation();
    const {servers, updateServer, refreshData} = useAppStore();
    const [isLoading, setIsLoading] = useState(false);
    const [authType, setAuthType] = useState<AuthType>('key');
    const [sshUnlock, setSshUnlock] = useState(false);
    const [unlockLoading, setUnlockLoading] = useState(false);
    const [unlockError, setUnlockError] = useState<string | undefined>();
    const server = servers.find(item => item.id === serverId);
    const {formData, updateField, resetForm} = useServerForm(server ? serverToFormData(server) : undefined);

    useEffect(() => {
        if (!server) return;
        const timeout = window.setTimeout(() => {
            resetForm(serverToFormData(server));
            setAuthType(authTypeForServer(server));
        }, 0);
        return () => window.clearTimeout(timeout);
    }, [server, resetForm]);

    if (!server) return <div className="p-8 text-center text-muted-foreground">{t('servers.noServers')}</div>;

    const handleSave = async () => {
        if (!formData.name.trim()) {
            toast.error(t('servers.nameRequired'));
            return;
        }
        setIsLoading(true);
        try {
            const updated = await updateServer(serverId, formDataToServerInput(formData, authType, server));
            if (!updated) {
                toast.error(t('servers.updateServerError'));
                return;
            }
            toast.success(t('servers.serverUpdated'));
            await refreshData();
            onChanged();
        } catch (error) {
            if (error instanceof SSHPassphraseRequiredError) {
                setSshUnlock(true);
                return;
            }
            toast.error(error instanceof Error ? error.message : t('servers.updateServerError'));
        } finally {
            setIsLoading(false);
        }
    };

    const handleUnlock = async (passphrase: string, applyToAll: boolean) => {
        setUnlockLoading(true);
        setUnlockError(undefined);
        try {
            if (await unlockServerSSH(serverId, passphrase, applyToAll)) {
                setSshUnlock(false);
                toast.success(t('servers.serverUpdated'));
                await refreshData();
                onChanged();
            } else {
                setUnlockError(t('auth.unlockError'));
            }
        } finally {
            setUnlockLoading(false);
        }
    };

    return (
        <div>
            {section === 'information' ? (
                <Container title={t('servers.information')}>
                    <form onSubmit={event => event.preventDefault()}>
                        <ServerFormFields
                            formData={formData}
                            onChange={updateField}
                            disabled={isLoading}
                            showAuth
                            authType={authType}
                            onAuthTypeChange={setAuthType}
                            serverId={server.id}
                            hasStoredKey={Boolean(server.ssh.keyData)}
                            hasTLSCertificates={Boolean(server.agent.tls?.ca?.cert)}
                            onCertificatesGenerated={async () => {
                                toast.success(t('servers.certificatesGenerated'));
                                await refreshData();
                                onChanged();
                            }}
                        />
                        <div className="flex gap-3 pt-6">
                            <button type="button" onClick={() => void handleSave()} disabled={isLoading} className={buttons.primary}>
                                {isLoading ? `${t('common.saving')}...` : t('common.save')}
                            </button>
                            <button type="button" onClick={() => {
                                resetForm(serverToFormData(server));
                                setAuthType(authTypeForServer(server));
                            }} disabled={isLoading} className={buttons.secondary}>{t('common.cancel')}</button>
                        </div>
                    </form>
                </Container>
            ) : <ResourceManager serverId={serverId} onChanged={onChanged} />}
            {sshUnlock && <SSHPassphraseModal onSubmit={handleUnlock} onClose={() => setSshUnlock(false)} loading={unlockLoading} error={unlockError} />}
        </div>
    );
}
