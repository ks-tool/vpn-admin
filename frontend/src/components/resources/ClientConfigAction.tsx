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
import {Download, FileDown} from 'lucide-react';
import {toast} from 'sonner';
import {CopyButton} from '@/components/common/CopyButton';
import {Modal} from '@/components/common/Modal';
import {buttons, inputs} from '@/components/common/controls';
import {reportError} from '@/services/errorReporting';
import {renderProviderClientConfig, saveActionResult, saveClientConfigQRCode} from '@/services/resources';
import {cn} from '@/lib/utils';
import type {ProviderClientConfig} from '@/types';
import type {ActionState} from './ResourceManager';

function safeFileName(value: string): string {
    return value.replace(/[/\\:*?"<>|]+/g, '_').split('').filter(character => {
        const code = character.charCodeAt(0);
        return code > 31 && code !== 127;
    }).join('').replace(/^[.\s]+|[.\s]+$/g, '') || 'peer';
}

export function ClientConfigAction({state, onClose}: {state: ActionState; onClose: () => void}) {
    const {t} = useTranslation();
    const [result, setResult] = useState<ProviderClientConfig | null>(null);
    const [error, setError] = useState('');
    const resource = state.item.resource;
    const name = typeof resource.spec.name === 'string' && resource.spec.name.length > 0 ? resource.spec.name : resource.name;

    useEffect(() => {
        let cancelled = false;
        void renderProviderClientConfig(resource.serverId, state.item.provider, resource.kind, resource.name, state.initialArguments ?? {}).then(value => {
            if (!cancelled) setResult(value);
        }).catch(cause => {
            const message = cause instanceof Error ? cause.message : String(cause);
            if (!cancelled) setError(message);
            reportError('render-client-config', t('peers.generateConfigError'), message);
        });
        return () => {cancelled = true;};
    }, [resource.serverId, resource.kind, resource.name, state.item.provider, state.initialArguments, t]);

    const saveConfig = async () => {
        if (!result) return;
        const fileName = `${safeFileName(name).slice(0, 15)}.conf`;
        await saveActionResult({type: 'file', fileName: result.fileName || fileName, mediaType: result.mediaType}, result.text);
        toast.success(t('peers.configSaved'));
    };
    const saveQRCode = async () => {
        if (!result) return;
        if (await saveClientConfigQRCode(safeFileName(name), result.qrCode)) toast.success(t('peers.qrCodeSaved'));
    };

    return (
        <Modal title={state.action.title} onClose={onClose} loading={!result && !error}>
            <div className="flex flex-col items-center gap-4">
                {!result && !error && <div className="py-12 text-sm text-muted-foreground dark:text-zinc-500">{t('common.generating')}</div>}
                {error && <div className="py-4 text-sm break-words text-red-500">{error}</div>}
                {result && (
                    <>
                        <img src={`data:image/png;base64,${result.qrCode}`} alt={t('peers.qrCodeTitle', {name})} className="rounded-lg bg-white p-2"/>
                        <div className="flex w-full items-center gap-2">
                            <textarea readOnly value={result.text} rows={8} className={cn(inputs.primary, 'resize-none font-mono text-xs')}/>
                            <CopyButton value={result.text}/>
                        </div>
                        <button type="button" onClick={() => void saveQRCode()} className={cn('inline-flex w-full items-center justify-center gap-2', buttons.primary)}>
                            <Download size={16}/>{t('peers.downloadQrCode')}
                        </button>
                        <button type="button" onClick={() => void saveConfig()} className={cn('inline-flex w-full items-center justify-center gap-2', buttons.secondary)}>
                            <FileDown size={16}/>{t('peers.saveConfig')}
                        </button>
                    </>
                )}
                <button type="button" onClick={onClose} className={cn('w-full', buttons.secondary)}>{t('common.close')}</button>
            </div>
        </Modal>
    );
}
