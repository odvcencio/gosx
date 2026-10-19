// Shared navigation projections and surveyed routes. Physics owns movement.
function sceneVesselNavigationReading(state: any, detail: any, route: any, passage: any, arrived: boolean): any {
    const degrees = (value: number) => String(Math.round(value) % 360).padStart(3, '0') + '°';
    return {
        NodeID: detail.nodeId, Mode: detail.mode, Following: detail.following, Position: { X: state.x, Y: state.y, Z: state.z }, Bearing: detail.bearing,
        Speed: detail.speed, Trim: detail.trim, Distance: detail.distance, Grounded: detail.grounded, WindFrom: detail.windFrom, WindStrength: detail.windStrength,
        Course: detail.course, Landing: detail.landing,
        Ready: true, Sailing: state.mode === 'sailing', Held: Number.isFinite(state.course), CanLand: !!detail.landing,
        BearingLabel: degrees(detail.bearing), SpeedLabel: state.speed.toFixed(1) + ' m/s', WindLabel: degrees(detail.windFrom) + ' · ' + state.strength.toFixed(0) + ' m/s',
        DistanceLabel: state.distance < 1000 ? Math.round(state.distance) + ' m' : (state.distance / 1000).toFixed(2) + ' km',
        HelmLabel: state.mode === 'sailing' ? (detail.following ? 'Leave helm' : 'Return to helm') : arrived ? 'Return to ship' : 'Pilot ship',
        SailLabel: state.trim > .95 ? 'Lower sail' : 'Full sail', HoldLabel: Number.isFinite(state.course) ? 'Release course' : 'Hold course', LandingLabel: detail.landing ? 'Step ashore · ' + detail.landing : 'Step ashore',
        Status: state.grounded ? 'Grounded · steer toward deeper water' : Number.isFinite(state.course) ? 'Holding ' + degrees(detail.course) + ' · voyage continues' : state.mode === 'sailing' ? 'Set a course or steer freely' : 'Board the ship to start your voyage.',
        Destination: !route ? 'Choose an island to plot its landing.' : arrived ? 'Explore ' + route.name : route.name + ' · ' + Math.round(Math.hypot(state.x - route.position.x, state.z - route.position.z)) + ' m' + (passage.length ? ' · safe passage' : ''),
        MapTransform: 'translate(' + state.x + ' ' + state.z + ') rotate(' + detail.bearing + ')', MapViewBox: [state.x - 80, state.z - 60, 160, 120].join(' ')
    };
}
function sceneCreateVesselRoutes(state: any, config: any, holdCourse: any, bearing: any): any {
    let route: any = null, passage: any[] = [], arrived = false, elapsed = 0;
    const to = (p: any) => bearing(Math.atan2(-((p.x || 0) - state.x), -((p.z || 0) - state.z)));
    return {
        get route() { return route; }, get passage() { return passage; }, get arrived() { return arrived; },
        select(id: string): boolean {
            const next = (config.routes || []).slice(0, 32).find((r: any) => r.id === id);
            if (!next)
                return false;
            route = next;
            arrived = false;
            state.course = NaN;
            const origin = config.position || {};
            passage = [...(Math.hypot(state.x - (origin.x || 0), state.z - (origin.z || 0)) < (next.departureRadius || 45) ? next.departure || [] : []), ...(state.z > (next.approachBeforeZ || 0) ? next.approach || [] : [])];
            return true;
        },
        clear() { route = null; passage = []; }, land() { arrived = true; },
        course(value: number): number { return route ? to(passage[0] || route.position) : value; },
        advance(dt: number): void {
            if (!route || !Number.isFinite(state.course) || arrived)
                return;
            elapsed += dt;
            if (passage.length && Math.hypot(state.x - (passage[0].x || 0), state.z - (passage[0].z || 0)) < 20)
                passage.shift();
            if (Math.hypot(state.x - (route.position.x || 0), state.z - (route.position.z || 0)) < 30)
                state.trim = 0;
            if (elapsed < 1)
                return;
            elapsed %= 1;
            holdCourse(state, to(passage[0] || route.position));
        }
    };
}
function sceneReserveVesselHUD(state: any, mount: any, listen: any): () => void {
    const anchor = state.config.controlsAnchorId && document.getElementById(state.config.controlsAnchorId), scope = mount.closest?.('[data-gosx-scene3d-control-scope]') || mount;
    const reserve = () => { if (anchor && scope.contains(anchor) && Number.isFinite(window.innerHeight))
        mount.style.setProperty('--gosx-vessel-controls-bottom', Math.max(14, window.innerHeight - anchor.getBoundingClientRect().top + 14) + 'px'); };
    const observer = anchor && typeof ResizeObserver === 'function' ? new ResizeObserver(reserve) : null;
    if (observer)
        observer.observe(anchor);
    if (anchor)
        listen(window, 'resize', reserve);
    reserve();
    return () => { if (observer)
        observer.disconnect(); };
}
