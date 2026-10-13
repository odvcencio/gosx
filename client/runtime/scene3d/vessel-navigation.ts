// Opt-in local chart instruments. Physics still owns collision and movement.
(function() {
  // @ts-ignore TS7006 -- bounded plain-JS angle helpers also run in physics tests.
  const angle=value=>Math.atan2(Math.sin(value),Math.cos(value));
  // @ts-ignore TS7006 -- compass degrees are serialized as finite numbers.
  const bearing=yaw=>(((-yaw*180/Math.PI)%360)+360)%360;
  // @ts-ignore TS7006 -- local commands are validated before changing the controller.
  function holdCourse(state,degrees) {
    if(!state.config.navigation || state.mode!=='sailing' || !Number.isFinite(degrees) || degrees<0 || degrees>360)return false;
    state.course=angle(-degrees*Math.PI/180);return true;
  }
  // @ts-ignore TS7006 -- this controller feeds the existing fixed-step rudder.
  function rudder(state,input) {
    const manual=input.rudder||0;
    if(Math.abs(manual)>.001 || state.mode!=='sailing')state.course=NaN;
    if(!Number.isFinite(state.course))return manual;
    return Math.max(-1,Math.min(1,-angle(state.course-state.heading)*3+state.yawVelocity*4));
  }
  // @ts-ignore TS7006 -- mount-local event adapter; no network authority.
  function setup(mount,state,config,helm,schedule) {
    let elapsed=.2;
    const commandEvent='gosx:scene3d:vessel-command';
    function publish() {
      mount.setAttribute('data-gosx-scene3d-vessel',state.mode);
      mount.setAttribute('data-gosx-scene3d-course-hold',String(Number.isFinite(state.course)));
      const detail={nodeId:config.nodeId,mode:state.mode,position:{x:state.x,y:state.y,z:state.z},bearing:bearing(state.heading),
        speed:state.speed,trim:state.trim,distance:state.distance,grounded:state.grounded,windFrom:bearing(state.wind),windStrength:state.strength,
        course:Number.isFinite(state.course)?bearing(state.course):null};
      mount.dispatchEvent(new CustomEvent('gosx:scene3d:vessel-state',{detail,bubbles:true}));
    }
    // @ts-ignore TS7006 -- exact node and finite bounds scope native commands.
    function command(event) {
      const d=event.detail;
      if(!d || typeof d!=='object' || d.nodeId!==config.nodeId)return;
      switch(d.action) {
        case 'helm': helm(true);break;
        case 'course': if(!holdCourse(state,d.bearing))return;break;
        case 'release-course': state.course=NaN;break;
        case 'trim':
          if(state.mode!=='sailing'||!Number.isFinite(d.trim)||d.trim<0||d.trim>1)return;
          state.trim=d.trim;break;
        default: return;
      }
      publish();schedule('vessel-command');
    }
    mount.addEventListener(commandEvent,command);
    return {
      // @ts-ignore TS7006 -- frame time stays live when the animation clock is frozen.
      advance(dt) {elapsed+=dt;if(elapsed<.2)return;elapsed%=.2;publish();},
      dispose() {mount.removeEventListener(commandEvent,command);},
    };
  }
  window.__gosx_scene3d_vessel_navigation={holdCourse,rudder,setup};
})();
