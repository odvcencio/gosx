// Opt-in local chart instruments. Physics still owns collision and movement.
  // @ts-ignore TS7006 -- bounded plain-JS sceneVesselNavigationAngle helpers also run in physics tests.
  const sceneVesselNavigationAngle=value=>Math.atan2(Math.sin(value),Math.cos(value));
  // @ts-ignore TS7006 -- compass degrees are serialized as finite numbers.
  const sceneVesselNavigationBearing=yaw=>(((-yaw*180/Math.PI)%360)+360)%360;
  // @ts-ignore TS7006 -- local commands are validated before changing the controller.
  function sceneVesselHoldCourse(state,degrees) {
    if(!state.config.navigation || state.mode!=='sailing' || !Number.isFinite(degrees) || degrees<0 || degrees>360)return false;
    state.course=sceneVesselNavigationAngle(-degrees*Math.PI/180);return true;
  }
  // @ts-ignore TS7006 -- this controller feeds the existing fixed-step sceneVesselNavigationRudder.
  function sceneVesselNavigationRudder(state,input) {
    const manual=input.rudder||0;
    if(Math.abs(manual)>.001 || state.mode!=='sailing')state.course=NaN;
    if(!Number.isFinite(state.course))return manual;
    return Math.max(-1,Math.min(1,-sceneVesselNavigationAngle(state.course-state.heading)*3+state.yawVelocity*4));
  }
  // @ts-ignore TS7006 -- mount-local event adapter; no network authority.
  function sceneVesselNavigationLanding(state) {
    if(state.mode!=='sailing'&&state.mode!=='deck')return null;
    let nearest=null,distance=Infinity;
    for(const site of state.config.landings||[]) {
      const p=site.position;
      if(!p||![p.x,p.y,p.z].every(Number.isFinite)||typeof site.id!=='string')continue;
      const d=Math.hypot(p.x-state.x,p.z-state.z);
      if(d<= (site.radius||28)&&state.speed<= (site.maxSpeed||1.5)&&d<distance){nearest=site;distance=d;}
    }
    return nearest;
  }
  // @ts-ignore TS7006 -- sceneVesselNavigationLanding delegates camera ownership to the mounted walker.
  function sceneSetupVesselNavigation(mount,state,config,helm,schedule,land=()=>false,signals=null) {
    let elapsed=.2;
    const routes=sceneCreateVesselRoutes(state,config,sceneVesselHoldCourse,sceneVesselNavigationBearing);
    const commandEvent='gosx:scene3d:vessel-command';
    function publish() {
      mount.setAttribute('data-gosx-scene3d-vessel',state.mode);
      mount.setAttribute('data-gosx-scene3d-course-hold',String(Number.isFinite(state.course)));
      mount.setAttribute('data-gosx-scene3d-vessel-following',String(state.mode==='sailing'&&!state.detached));
      const detail={nodeId:config.nodeId,mode:state.mode,following:state.mode==='sailing'&&!state.detached,position:{x:state.x,y:state.y,z:state.z},bearing:sceneVesselNavigationBearing(state.heading),
        speed:state.speed,trim:state.trim,distance:state.distance,grounded:state.grounded,windFrom:sceneVesselNavigationBearing(state.wind),windStrength:state.strength,
        course:Number.isFinite(state.course)?sceneVesselNavigationBearing(state.course):null,landing:sceneVesselNavigationLanding(state)?.id||null};
      mount.dispatchEvent(new CustomEvent('gosx:scene3d:vessel-state',{detail,bubbles:true}));
      if(config.stateSignal&&signals?.publish)signals.publish(config.stateSignal,sceneVesselNavigationReading(state,detail,routes.route,routes.passage,routes.arrived));
    }
    // @ts-ignore TS7006 -- exact node and finite bounds scope native commands.
    function command(event) {
      const d=event.detail;
      if(!d || typeof d!=='object' || d.nodeId!==config.nodeId)return;
      switch(d.action) {
        case 'helm': helm(true);break;
        case 'course': {const target=typeof d.bearingInput==='string'&&/^\d{1,3}(?:\.\d{1,6})?$/.test(d.bearingInput)?Number(d.bearingInput):d.bearing;if(!sceneVesselHoldCourse(state,routes.course(target)))return;break;}
        case 'manual-course': routes.clear();if(!sceneVesselHoldCourse(state,d.bearing))return;break;
        case 'release-course': state.course=NaN;break;
        case 'center-view': state.lookYaw=state.lookPitch=0;break;
        case 'trim':
          if(state.mode!=='sailing'||!Number.isFinite(d.trim)||d.trim<0||d.trim>1)return;
          state.trim=d.trim;break;
        case 'land': if(!land())return;routes.land();break;
        default: return;
      }
      publish();schedule('vessel-command');
    }
    mount.addEventListener(commandEvent,command);
    const unsubscribe=config.commandSignal&&signals?.subscribe?signals.subscribe(config.commandSignal,value=>{
      if(!value||typeof value!=='object')return;
      command({detail:{nodeId:config.nodeId,action:value.Action,bearing:value.Bearing,bearingInput:value.BearingInput,trim:value.Trim}});
    }):()=>{};
    const unsubscribeDestination=config.destinationSignal&&signals?.subscribe?signals.subscribe(config.destinationSignal,value=>{if(typeof value==='string'&&value&&routes.select(value)){publish();schedule('vessel-route');}else if(value===''){routes.clear();}}):()=>{};
    return {
      publish,
      // @ts-ignore TS7006 -- frame time stays live when the animation clock is frozen.
      advance(dt) {routes.advance(dt);elapsed+=dt;if(elapsed<.2)return;elapsed%=.2;publish();},
      dispose() {unsubscribe();unsubscribeDestination();mount.removeEventListener(commandEvent,command);},
    };
  }
  window.__gosx_scene3d_vessel_navigation={holdCourse:sceneVesselHoldCourse,rudder:sceneVesselNavigationRudder,landing:sceneVesselNavigationLanding,setup:sceneSetupVesselNavigation};
