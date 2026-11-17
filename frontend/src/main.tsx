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

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './index.css';
import App from './App';
import './i18n/config';
import {logError} from './services/errorReporting';

window.addEventListener('error', event => {
    const error = event.error instanceof Error ? event.error.message : event.message;
    logError('Unhandled UI error', error);
});
window.addEventListener('unhandledrejection', event => {
    const error = event.reason instanceof Error ? event.reason.message : String(event.reason);
    logError('Unhandled UI promise rejection', error);
});

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <App />
    </StrictMode>
);