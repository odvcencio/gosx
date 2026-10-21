// Lazy vessel authority. The mount calls advance exactly once per rendered frame.
(function() {
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function loadFloor(ocean, accept, current) {
    const mapping=ocean.bathymetry;if(!mapping||!mapping.src||typeof Image!=='function')return;
    const image=new Image();image.crossOrigin='anonymous';
    image.onload=()=>{
      if(!current())return;
      const canvas=document.createElement('canvas');canvas.width=image.width;canvas.height=image.height;
      const context=canvas.getContext('2d',{willReadFrequently:true});if(!context)return;
      try {context.drawImage(image,0,0);accept(window.__gosx_scene3d_ocean_query.bathymetry(context.getImageData(0,0,image.width,image.height),mapping));}
      catch(error) { /* Terrain remains a deterministic collision fallback. */ }
    };
    image.src=mapping.src;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function deck(state, physics, surface) {
    const centre=physics.localPoint(state,0,state.deck,0);
    const x=physics.localPoint(state,1,state.deck,0),z=physics.localPoint(state,0,state.deck,1);
    surface.x=centre.x;surface.y=centre.y;surface.z=centre.z;surface.rotationY=state.heading;
    surface.slopeX=x.y-centre.y;surface.slopeZ=z.y-centre.y;
    surface.sizeX=state.beam*.78;surface.sizeZ=state.length*.74;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function setup(canvas,props,base,sceneState,helpers) {
    const physics=window.__gosx_scene3d_vessel_physics,query=window.__gosx_scene3d_ocean_query;
    const ocean=sceneState.environment.ocean||{},walk=props.walk||{},state=physics.create(props.vessel,ocean,walk);
    // ExternalPose: Go owns the pose; no sailing physics runs and nobody can take the helm.
    const ext=props.vessel.externalPose?window.__gosx_scene3d_vessel_external:null;
    if(ext)state.mode='deck';
    const motion=typeof window.matchMedia==='function'?window.matchMedia('(prefers-reduced-motion: reduce)'):null;
    const walkAPI=window.__gosx_scene3d_walk_api,ground=walkAPI?walkAPI.decodeGround(walk.ground):null,outsideHeight=props.vessel.openWater?-1e4:undefined;
    const grounds=walkAPI?(walk.grounds||[]).map(walkAPI.decodeGround):[];
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    const floor=(x,z)=>walkAPI&&(ground||grounds.length)?walkAPI.sampleGrounds(ground,grounds,x,z,outsideHeight):-1e4;
    let waveFloor=()=>-1e4,disposed=false,first=true,lastBob=0;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    const high=query.create(ocean,'high',(x,z)=>waveFloor(x,z),true),low=query.create(ocean,'low',(x,z)=>waveFloor(x,z),true);
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    loadFloor(ocean,sample=>{waveFloor=sample;helpers.schedule('vessel-bathymetry');},()=>!disposed);
    const mount=canvas.closest('[data-gosx-engine="GoSXScene3D"]')||canvas.parentElement;
    const model=window.__gosx_scene3d_vessel_model.create(sceneState,props.vessel,physics),surface={x:0,y:0,z:0,sizeX:0,sizeZ:0,rotationY:0,slopeX:0,slopeZ:0};
    const decks=props.vessel.decks?.length||props.vessel.cutaway||ext?window.__gosx_scene3d_vessel_decks.create(props.vessel.decks||[],physics):null,multi=!!decks&&decks.list.length>0,floor0=multi?decks.surface:surface;
    const cw=decks?window.__gosx_scene3d_vessel_cutaway_api.create(decks,mount,()=>helpers.schedule('vessel-cutaway'),props.vessel.cutaway):null;
    if(cw){model.cut=cw.touch;window.__gosx_scene3d_vessel_cutaway_api.register(mount.id,props.vessel.nodeId,cw);}
    if(props.walk) {walk.surfaces=walk.surfaces||[];walk.surfaces.push(floor0);}
    const link=ext?ext.attach(mount,props.vessel.nodeId,()=>helpers.schedule('vessel-pose')):null;
    const read=()=>helpers.current(base.controller,sceneState.camera,sceneState._scrollCamera);
    let navigation: any = null;
    const controller={get zoomScale(){return base.controller.zoomScale||1;},set zoomScale(value: number){base.controller.zoomScale=value;},mode:'first-person',touched:false,get active(){return state.mode==='sailing'||!!base.controller?.active||(link&&link.active());},currentCamera:()=>state.mode==='sailing'&&!state.detached?state.camera||read():read(),
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      syncCamera:camera=>base.controller?.syncCamera?.(camera),
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      applyCamera:camera=>{if(state.mode==='sailing'&&props.vessel.navigation){state.detached=true;window.__gosx_scene3d_vessel_input.clear(controls.input);suspend(false);navigation?.publish();}else leave();base.controller?.applyCamera?.(camera);}};
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    function suspend(value) {
      if(base.controller&&base.controller.setSuspended)base.controller.setSuspended(value);
      else if(base.controller)base.controller.suspended=value;
    }
    function leave() {state.mode='deck';state.detached=false;state.trim=0;state.rudder=0;state.course=NaN;suspend(false);}
    function changeHelm(quick=false) {
      if(link)return;
      const result=state.mode==='sailing'&&state.detached?(physics.canBoard(state,read())||(quick&&state.config.quickBoard)?'take':'far'):physics.helm(state,read(),quick);if(result==='far')return;
      window.__gosx_scene3d_vessel_input.clear(controls.input);
      if(result==='take') {state.detached=false;state.cameraReady=false;suspend(true);controller.touched=true;if(quick)state.cameraMode='wheel';state.camera=physics.camera(state,read(),.1);}
      else {leave();if(base.controller&&base.controller.applyCamera)base.controller.applyCamera(Object.assign({},read(),physics.localPoint(state,state.helm.x,state.helm.y,state.helm.z),{rotationY:state.heading,rotationX:0}));}
      helpers.schedule('vessel-helm');
    }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    const controls=link?link.controls:window.__gosx_scene3d_vessel_input.setup(canvas,mount,state,{helm:changeHelm,camera:()=>{physics.toggleCamera(state);state.camera=null;helpers.schedule('vessel-camera');},look:(yaw: number,pitch: number)=>{physics.look(state,yaw,pitch);helpers.schedule('vessel-look');},reset});
    function land() {
      const site=window.__gosx_scene3d_vessel_navigation.landing(state);
      if(!site)return false;
      leave();state.mode='moored';state.vx=state.vz=state.speed=0;
      base.controller?.applyCamera?.(Object.assign({},read(),site.position,{rotationX:0,rotationY:0}));
      return true;
    }
    navigation=props.vessel.navigation&&!link?window.__gosx_scene3d_vessel_navigation.setup(mount,state,props.vessel,changeHelm,helpers.schedule,land,helpers):null;
    if(base.bindReset)base.bindReset(reset);
    sceneState._gosxMotionController=controller;
    const wake=window.__gosx_scene3d_vessel_wake&&props.vessel.wake!==false?window.__gosx_scene3d_vessel_wake.create(sceneState,helpers,props.vessel):null;
    function reset() {leave();window.__gosx_scene3d_vessel_input.clear(controls.input);if(wake)wake.reset();if(base.reset)base.reset();Object.assign(state,physics.create(props.vessel,ocean,walk));if(link){state.mode='deck';link.reset();}physics.presentation(state,true);first=true;lastBob=0;helpers.schedule('vessel-reset');}
  // @ts-ignore TS7006 -- plain JS method parameters are exercised without transpilation.
    return {controller,state,zoomController:base.controller,reset,cancelTouch:()=>{if(base.cancelTouch)base.cancelTouch();window.__gosx_scene3d_vessel_input.clear(controls.input);},stopInertia:()=>false,advance(dt,seconds,detail,paused,waterSeconds=seconds) {
      if(disposed)return;
      state.reduced=!!(motion&&motion.matches);
      const camera=read(),aboard=state.mode==='deck'&&!first;
      const local=aboard?window.__gosx_scene3d_vessel_model.inversePoint(model.pose,Object.assign({},camera,{y:camera.y-lastBob})):null;
      lastBob=0;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      const q=helpers.lowHardware()?low:high,sample=(x,z)=>query.sample(q,x,z,waterSeconds);
      if(first) {if(!link)state.y=sample(state.x,state.z).y;physics.presentation(state,true);first=false;}
      const input=link?link.input:window.__gosx_scene3d_vessel_input.value(controls.input,true);
      physics.look(state,input.lookYaw*Math.min(.1,dt)*1.4,input.lookPitch*Math.min(.1,dt)*1.4);
      if(link)link.apply(state);
      else if(!paused)physics.advance(state,dt,seconds,input,sample,floor);
      const pose=link?state:physics.presentation(state);
      if(decks)decks.update(pose);if(!multi)deck(pose,physics,surface);if(cw)cw.update(decks);
      if(local&&(multi?decks.aboard(local):Math.abs(local.x)<surface.sizeX/2+.3&&Math.abs(local.z)<surface.sizeZ/2+.3&&Math.abs(local.y-state.helm.y)<2)) {
        const p=physics.localPoint(pose,local.x,local.y,local.z);
        lastBob=link?0:physics.deckBob(state,seconds);p.y+=lastBob;
        if(link&&base.controller)base.controller.touched=true;// an untouched walker would be reset to the scene camera
        if(base.controller&&base.controller.carryCamera)base.controller.carryCamera(Object.assign({},camera,p));
      }
      if(state.mode==='sailing'&&!state.detached)state.camera=physics.camera(state,camera,dt,seconds,pose);
      model.update(pose,controller.currentCamera(),seconds,detail);
      if(wake)wake.update(pose,seconds,sample,detail,controller.currentCamera());
      controls.refresh(state.config.quickBoard||physics.canBoard(state,read()));
      if(mount.getAttribute('data-gosx-scene3d-vessel')!==state.mode)mount.setAttribute('data-gosx-scene3d-vessel',state.mode);
      if(navigation)navigation.advance(dt);
    },dispose() {
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      disposed=true;if(cw)window.__gosx_scene3d_vessel_cutaway_api.unregister(mount.id,props.vessel.nodeId,cw);if(link)link.dispose();if(navigation)navigation.dispose();controls.dispose();if(wake)wake.dispose();if(props.walk)walk.surfaces=walk.surfaces.filter(s=>s!==floor0);
      suspend(false);base.dispose();if(sceneState._gosxMotionController===controller)sceneState._gosxMotionController=null;
    }};
  }
  window.__gosx_scene3d_vessel_api={setup};
})();
