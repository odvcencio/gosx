// Load a typed experience descriptor before controls acquire scene ownership.
async function sceneLoadExperience(props: any, signal: any): Promise<any> {
    if (!props.experienceRef)
        return props;
    const url = new URL(props.experienceRef, location.href);
    if (url.origin !== location.origin)
        throw new Error('Scene3D experience must be same-origin');
    const response = await fetch(url.href, { signal });
    if (!response.ok)
        throw new Error('Scene3D experience unavailable');
    const text = await response.text();
    if (text.length > 131072)
        throw new Error('Scene3D experience exceeds descriptor budget');
    const data = JSON.parse(text);
    if (!data || typeof data !== 'object' || Array.isArray(data))
        throw new Error('Invalid Scene3D experience');
    const next = Object.assign({}, props);
    for (const key of ['optics', 'presentation', 'dayCycle', 'exploration', 'vessel'])
        if (Object.prototype.hasOwnProperty.call(data, key))
            next[key] = data[key];
    return next;
}
// Typed presentation requests preserve the mounted simulation and its clock.
function sceneSetupPresentation(mount: any, props: any, handle: any, schedule: any): () => void {
    const config = props.presentation;
    if (!config || typeof window.__gosx_runtime_api.gosxSubscribeSharedSignal !== 'function')
        return () => { };
    let view = String(config.initialView || ''), period = String(config.initialPeriod || '');
    let disposed = false, revision = 0, requestID = 0, controller: AbortController | null = null;
    const scope = mount.closest('[data-gosx-scene3d-control-scope]') || mount;
    function publish(): void {
        if (config.outputSignal)
            window.__gosx_runtime_api.setSharedSignalValue(config.outputSignal, { Ready: true, View: view, Period: period });
        scope.setAttribute('data-view', view);
        scope.setAttribute('data-period', period);
    }
    async function request(value: any, historyChange = true): Promise<void> {
        if (disposed || !value || typeof value !== 'object')
            return;
        const selectedView = (config.views || []).find((v: any) => v.id === value.View);
        const selectedPeriod = (config.periods || []).find((p: any) => p.id === value.Period);
        if (selectedPeriod && selectedPeriod.id !== period) {
            const token = ++requestID;
            controller?.abort();
            controller = new AbortController();
            let commands = selectedPeriod.commands || [];
            if (config.endpoint) {
                const url = new URL(config.endpoint, location.href);
                if (url.origin !== location.origin)
                    return;
                url.searchParams.set('view', selectedView?.id || view);
                url.searchParams.set('period', selectedPeriod.id);
                url.searchParams.set('from', period);
                try {
                    const response = await fetch(url.href, { signal: controller.signal });
                    if (!response.ok)
                        return;
                    const next = await response.json();
                    if (!Array.isArray(next.commands) || token !== requestID || disposed)
                        return;
                    commands = next.commands;
                }
                catch (_) {
                    return;
                }
            }
            await handle.applyCommands(commands);
            if (disposed || token !== requestID)
                return;
            period = selectedPeriod.id;
            if (handle.setDayHour && Number.isFinite(selectedPeriod.hour))
                handle.setDayHour(selectedPeriod.hour);
        }
        if (selectedView) {
            const sailing = mount.getAttribute('data-gosx-scene3d-vessel') === 'sailing';
            const following = mount.getAttribute('data-gosx-scene3d-vessel-following') === 'true';
            const command = (action: string) => mount.dispatchEvent(new CustomEvent('gosx:scene3d:vessel-command', { detail: { nodeId: props.vessel?.nodeId, action } }));
            if (selectedView.vessel && sailing) {
                if (!following)
                    command('helm');
                if (value.Reset)
                    command('center-view');
            }
            else
                handle.setCamera(selectedView.camera);
            view = selectedView.id;
            const canvas = mount.querySelector('canvas');
            if (canvas)
                canvas.setAttribute('aria-label', selectedView.label);
            if (document.pointerLockElement)
                document.exitPointerLock();
        }
        publish();
        schedule('presentation');
        if (config.query && historyChange) {
            const url = new URL(location.href);
            url.searchParams.set('view', view);
            url.searchParams.set('period', period);
            if (url.href !== location.href)
                history.pushState(null, '', url);
        }
    }
    const unsubscribe = window.__gosx_runtime_api.gosxSubscribeSharedSignal(config.inputSignal, (value: any) => {
        if (!value || !Number.isSafeInteger(value.Revision) || value.Revision <= revision)
            return;
        revision = value.Revision;
        request(value);
    }, { immediate: false });
    const pop = () => { const q = new URL(location.href).searchParams; request({ View: q.get('view') || config.initialView, Period: q.get('period') || config.initialPeriod }, false); };
    const home = (event: KeyboardEvent) => {
        if (event.code !== 'Home' || event.target !== mount.querySelector('canvas'))
            return;
        event.preventDefault();
        event.stopImmediatePropagation();
        request({ View: view, Reset: true });
    };
    if (config.query)
        window.addEventListener('popstate', pop);
    mount.addEventListener('keydown', home, true);
    publish();
    return () => { disposed = true; controller?.abort(); unsubscribe(); window.removeEventListener('popstate', pop); mount.removeEventListener('keydown', home, true); };
}
// Slow environmental changes share the pausable scene clock and renderer state.
function sceneCreateDayCycle(config: any, state: any, hashes?: { light: any; environment: any }): any {
    if (!config || !state.environment?.sky || state.environment.sky.mode !== 'physical')
        return null;
    const duration = Number.isFinite(config.durationSeconds) && config.durationSeconds >= 60 ? Math.min(86400, config.durationSeconds) : 1080;
    const peak = Math.min(89, Math.max(1, Number(config.peakElevation) || 55));
    const intensity = Math.min(30, Math.max(0, Number(config.sunIntensity) || 9));
    const nightIntensity = Math.min(30, Math.max(0, Number(config.nightIntensity) || .22));
    const nightEnvironment = Math.min(4, Math.max(0, Number(config.nightEnvIntensity) || .025));
    const phase = (Number(config.seed) >>> 0) / 4294967296 * Math.PI * 2;
    let hour = Math.max(0, Math.min(23.999, Number(config.initialHour) || 0)), automatic = true, last = 0, next = -1;
    let publishedHour = NaN, publishedWeather = '', publishedAutomatic: boolean | undefined;
    const unsubscribe = config.inputSignal && typeof window.__gosx_runtime_api.gosxSubscribeSharedSignal === 'function' ? window.__gosx_runtime_api.gosxSubscribeSharedSignal(config.inputSignal, (value: any) => { if (typeof value === 'boolean')
        automatic = value; }) : () => { };
    const mix = (a: number, b: number, t: number) => a + (b - a) * t;
    const smooth = (a: number, b: number, x: number) => { const t = Math.max(0, Math.min(1, (x - a) / (b - a))); return t * t * (3 - 2 * t); };
    const color = (a: number[], b: number[], t: number) => '#' + a.map((n, i) => Math.round(mix(n, b[i], t)).toString(16).padStart(2, '0')).join('');
    function advance(seconds: number): void {
        if (!Number.isFinite(seconds))
            return;
        const dt = Math.max(0, seconds - last);
        last = seconds;
        if (automatic)
            hour = (hour + dt * 24 / duration) % 24;
        if (seconds < next)
            return;
        next = seconds + .2;
        const elevation = Math.sin((hour - 6) * Math.PI / 12) * peak;
        const daytime = smooth(-6, 4, elevation), midday = smooth(3, 35, elevation);
        const azimuth = ((Number(config.sunAzimuth) || 90) + (hour - 12) * 4) * Math.PI / 180, rad = elevation * Math.PI / 180;
        const sun = { x: Math.sin(azimuth) * Math.cos(rad), y: Math.sin(rad), z: -Math.cos(azimuth) * Math.cos(rad) };
        const env = state.environment, sky = env.sky;
        sky.sunDirection = sun;
        sky.intensity = mix(.05, .65, daytime);
        sky.turbidity = mix(3.2, 3.5, midday);
        sky.rayleigh = mix(1.4, 1.2, midday);
        env.envIntensity = mix(nightEnvironment, .85, daytime);
        env.fogColor = color([9, 18, 36], [153, 179, 184], daytime * (.65 + .35 * midday));
        const weather = .5 + .32 * Math.sin(seconds / 170 + phase) + .12 * Math.sin(seconds / 61 + phase * 1.7);
        const coverage = Math.max(.18, Math.min(.88, weather));
        if (config.weather && sky.clouds) {
            sky.clouds.coverage = coverage;
            sky.clouds.opacity = mix(.55, .84, coverage);
        }
        if (config.weather && env.haze)
            env.haze.density = mix(.00065, .0016, coverage);
        const light = state.lights instanceof Map ? state.lights.get(config.sunLightId) : null;
        if (light) {
            light.directionX = -sun.x;
            light.directionY = -sun.y;
            light.directionZ = -sun.z;
            light.intensity = intensity * daytime * mix(.71, 1, midday);
            light.color = color([255, 188, 133], [255, 244, 230], midday);
            if (typeof hashes?.light === 'function')
                light._lightHash = hashes.light(light);
        }
        const nightLight = state.lights instanceof Map ? state.lights.get(config.nightLightId) : null;
        if (nightLight && nightLight !== light) {
            nightLight.directionX = sun.x;
            nightLight.directionY = sun.y;
            nightLight.directionZ = sun.z;
            nightLight.intensity = nightIntensity * (1 - daytime) * smooth(0, 18, -elevation);
            if (typeof hashes?.light === 'function')
                nightLight._lightHash = hashes.light(nightLight);
        }
        if (typeof hashes?.environment === 'function')
            env._envHash = hashes.environment(env);
        if (config.outputSignal && typeof window.__gosx_runtime_api.setSharedSignalValue === 'function') {
            const weatherLabel = !config.weather ? 'Fair weather' : coverage > .68 ? 'Clouds gathering' : coverage < .38 ? 'Clear spells' : 'Coastal clouds';
            if (hour === publishedHour && weatherLabel === publishedWeather && automatic === publishedAutomatic)
                return;
            publishedHour = hour;
            publishedWeather = weatherLabel;
            publishedAutomatic = automatic;
            const minutes = Math.floor(hour * 60), label = String(Math.floor(minutes / 60)).padStart(2, '0') + ':' + String(minutes % 60).padStart(2, '0');
            window.__gosx_runtime_api.setSharedSignalValue(config.outputSignal, { Hour: hour, HourLabel: label, WeatherLabel: weatherLabel, Automatic: automatic });
        }
    }
    return { advance, seek: (value: number) => { if (Number.isFinite(value) && value >= 0 && value < 24) {
            hour = value;
            next = -1;
        } }, dispose: unsubscribe };
}
function sceneSetupExploration(mount: any, props: any, state: any, handle: any, schedule: any, audio: any, apply: any): any {
    const config = props.exploration;
    if (!config)
        return null;
    const scope = mount.closest('[data-gosx-scene3d-control-scope]') || mount;
    const items: any[] = (Array.isArray(config.items) ? config.items : []).slice(0, 128);
    const found = new Set<string>(), collected = new Set<string>(), listeners: Array<[
        any,
        string,
        any
    ]> = [];
    let active = '', notice = 'Explore at your own pace.', pulse: any = null, clock = 0, requestRevision = 0, lastSound = -Infinity;
    let frame: any = null, pocketOpen = false, resumeBackground = false;
    const pocket = config.pocket;
    const dialog = pocket && document.getElementById(pocket.dialogId);
    const validDialog: HTMLDialogElement | null = dialog && scope.contains(dialog) && dialog.tagName === 'DIALOG' ? dialog as HTMLDialogElement : null;
    const allowedSource = pocket && new URL(pocket.source, location.href);
    try {
        const saved = JSON.parse(localStorage.getItem(config.storageKey) || '[]');
        if (Array.isArray(saved))
            for (const id of saved)
                if (items.some(i => i.id === id))
                    found.add(id);
    }
    catch (_) { }
    function listen(target: any, event: string, fn: any): void { target.addEventListener(event, fn); listeners.push([target, event, fn]); }
    function publish(): void {
        const record: any = {};
        for (const id of found)
            record[id] = true;
        if (config.outputSignal)
            window.__gosx_runtime_api.setSharedSignalValue(config.outputSignal, { Found: record, CountLabel: found.size + ' / ' + items.length, ActiveID: active, Notice: notice, Pocket: items.some(i => i.collect && found.has(i.id)) });
        try {
            if (config.storageKey)
                localStorage.setItem(config.storageKey, JSON.stringify([...found]));
        }
        catch (_) { }
    }
    function applyCollected(): void {
        if (mount.getAttribute('data-gosx-scene3d-model-hydration-status') !== 'committed')
            return;
        for (const item of items)
            if (item.collect && found.has(item.id) && !collected.has(item.id)) {
                collected.add(item.id);
                handle.applyCommands(item.commands || []).catch(() => { collected.delete(item.id); });
            }
    }
    function send(action: string): void { if (frame?.contentWindow)
        frame.contentWindow.postMessage({ type: pocket.protocol, action }, location.origin); }
    function open(value: any): void {
        if (typeof value !== 'boolean' || !validDialog || !allowedSource || allowedSource.origin !== location.origin)
            return;
        pocketOpen = value;
        if (value) {
            if (document.pointerLockElement)
                document.exitPointerLock();
            if (!frame) {
                frame = document.createElement('iframe');
                frame.src = allowedSource.href;
                frame.title = pocket.title;
                frame.setAttribute('allow', 'autoplay');
                listen(frame, 'load', () => send(pocketOpen ? 'resume' : 'pause'));
                validDialog.appendChild(frame);
            }
            if (!validDialog.open) {
                const held = mount.getAttribute('data-gosx-scene3d-vessel') === 'sailing' && mount.getAttribute('data-gosx-scene3d-course-hold') === 'true', clock = handle.getAnimationClock();
                resumeBackground = !!pocket.pauseBackground && !held && !clock.paused;
                if (resumeBackground)
                    handle.setAnimationClock({ timeSeconds: clock.timeSeconds, paused: true });
                validDialog.showModal();
            }
            send('resume');
        }
        else {
            send('pause');
            if (validDialog.open)
                validDialog.close();
        }
    }
    function observe(id: any): void {
        if (typeof id !== 'string')
            return;
        const item = items.find(i => i.id === id.split('/')[0]);
        if (!item)
            return;
        const camera = handle.getCamera(), p = item.position;
        if (p && (!camera || Math.hypot(camera.x - p.x, camera.z - p.z) > (item.radius || 7))) {
            notice = 'Move closer to this discovery.';
            publish();
            return;
        }
        found.add(item.id);
        active = item.id;
        notice = item.label + ' · recorded in your field journal.';
        if (item.pulse && !window.matchMedia?.('(prefers-reduced-motion: reduce)').matches)
            pulse = { config: item.pulse, start: clock };
        if (item.sound && clock - lastSound > .7) {
            lastSound = clock;
            audio({ patch: item.sound });
        }
        if (document.pointerLockElement)
            document.exitPointerLock();
        publish();
        applyCollected();
        if (!item.collect && config.revealSignal) {
            window.__gosx_runtime_api.setSharedSignalValue(config.revealSignal, true);
            for (const name of (config.dismissSignals || []).slice(0, 8))
                window.__gosx_runtime_api.setSharedSignalValue(name, false);
        }
        if (item.collect && pocket)
            window.__gosx_runtime_api.setSharedSignalValue(pocket.openSignal, true);
        schedule('inspection');
    }
    listen(mount, 'gosx:scene3d:input', (event: any) => { if (event.detail?.kind === 'pick' && event.detail?.input?.type === 'select')
        observe(event.detail.input.targetID); });
    listen(mount, 'gosx:scene3d:model-hydration-status', applyCollected);
    listen(scope, 'click', (event: any) => { const target = event.target.closest?.('[data-gosx-scene-node]'); if (target && scope.contains(target))
        observe(target.getAttribute('data-gosx-scene-node')); });
    const unsubRequest = config.inputSignal ? window.__gosx_runtime_api.gosxSubscribeSharedSignal(config.inputSignal, (value: any) => {
        if (value && Number.isSafeInteger(value.Revision) && value.Revision > requestRevision) {
            requestRevision = value.Revision;
            observe(value.ID);
        }
    }, { immediate: false }) : () => { };
    const unsubPocket = pocket ? window.__gosx_runtime_api.gosxSubscribeSharedSignal(pocket.openSignal, open) : () => { };
    if (validDialog) {
        listen(validDialog, 'close', () => { pocketOpen = false; send('pause'); if (resumeBackground) {
            const clock = handle.getAnimationClock();
            handle.setAnimationClock({ timeSeconds: clock.timeSeconds, paused: false });
        } resumeBackground = false; window.__gosx_runtime_api.setSharedSignalValue(pocket.openSignal, false); });
        listen(window, 'message', (event: any) => {
            if (event.origin !== location.origin || !frame || event.source !== frame.contentWindow)
                return;
            if (event.data?.type === pocket.protocol + '-close')
                open(false);
            if (event.data?.type === pocket.protocol + '-ready')
                send(pocketOpen ? 'resume' : 'pause');
            if (event.data?.type === pocket.protocol + '-saved') {
                notice = event.data.ok ? 'Handheld put away · checkpoint saved on this device.' : 'Handheld put away · this session can resume; autosave is unavailable.';
                publish();
            }
        });
        listen(window, 'pagehide', () => send('pause'));
        listen(window, 'pageshow', () => send(pocketOpen ? 'resume' : 'pause'));
    }
    publish();
    applyCollected();
    return {
        get active() { return !!pulse; },
        advance(seconds: number): void {
            clock = seconds;
            if (!pulse)
                return;
            const c = pulse.config, t = Math.max(0, Math.min(1, (seconds - pulse.start) / (c.durationSeconds || 1.1))), scale = t === 1 ? .001 : c.radius * (.12 + t * .75);
            apply([{ kind: 'setTransform', objectId: c.nodeId, data: { x: c.position.x, y: c.position.y, z: c.position.z, rotationX: Math.PI / 2, rotationY: 0, rotationZ: 0, scaleX: scale, scaleY: scale, scaleZ: Math.max(.001, 1 - t), parentMatrix: null } }]);
            if (t === 1)
                pulse = null;
        },
        dispose(): void { unsubRequest(); unsubPocket(); send('pause'); for (const [target, event, fn] of listeners)
            target.removeEventListener(event, fn); if (frame)
            frame.remove(); },
    };
}
function sceneMetricsSignal(name: any, read: any): any {
    if (!name || typeof window.__gosx_runtime_api.setSharedSignalValue !== 'function')
        return () => { };
    const intervals: number[] = [], frames: Array<[
        number,
        number
    ]> = [];
    let next = 0, previousSubmit = 0, previousSequence = 0, previousSample = 0;
    return () => {
        const now = performance.now();
        if (now < next)
            return;
        next = now + 239;
        if (document.hidden) {
            intervals.length = frames.length = 0;
            previousSubmit = previousSequence = previousSample = 0;
            return;
        }
        let transfer = 0;
        for (const kind of ['navigation', 'resource'])
            for (const entry of performance.getEntriesByType(kind))
                if (!entry.name.startsWith('blob:'))
                    transfer += (entry as PerformanceResourceTiming).transferSize || 0;
        const timing = read();
        if (!timing || !(timing.submitAtMS > 0))
            return;
        if (timing.submitAtMS !== previousSubmit && timing.frameIntervalMS > 0) {
            intervals.push(timing.frameIntervalMS);
            if (intervals.length > 24)
                intervals.shift();
        }
        previousSubmit = timing.submitAtMS;
        let rate = intervals.length ? 1000 / (intervals.reduce((a, b) => a + b, 0) / intervals.length) : 0;
        const sequence = Number(timing.frameSequence);
        if (Number.isFinite(sequence) && sequence > 0) {
            if (sequence < previousSequence) {
                frames.length = 0;
                previousSequence = previousSample = 0;
            }
            if (previousSequence && sequence > previousSequence && now > previousSample) {
                frames.push([sequence - previousSequence, now - previousSample]);
                if (frames.length > 12)
                    frames.shift();
            }
            previousSequence = sequence;
            previousSample = now;
            if (frames.length)
                rate = 1000 * frames.reduce((a, b) => a + b[0], 0) / frames.reduce((a, b) => a + b[1], 0);
        }
        window.__gosx_runtime_api.setSharedSignalValue(name, { Ready: true, TransferLabel: Math.round(transfer / 1024).toLocaleString(), FPSLabel: now - timing.submitAtMS > 1000 ? '0' : rate > 0 ? String(Math.round(rate)) : '…' });
    };
}
window.__gosx_runtime_api.scene3DExperience = { load: sceneLoadExperience, presentation: sceneSetupPresentation, cycle: sceneCreateDayCycle, exploration: sceneSetupExploration, metrics: sceneMetricsSignal };
