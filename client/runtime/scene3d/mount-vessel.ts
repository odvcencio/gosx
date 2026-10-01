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
    const motion=typeof window.matchMedia==='function'?window.matchMedia('(prefers-reduced-motion: reduce)'):null;
    const walkAPI=window.__gosx_scene3d_walk_api,ground=walk.ground&&walkAPI?walkAPI.decodeGround(walk.ground):null;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    const floor=(x,z)=>ground?walkAPI.sampleGround(ground,x,z):-1e4;
    let waveFloor=()=>-1e4,disposed=false,first=true,lastBob=0;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    const high=query.create(ocean,'high',(x,z)=>waveFloor(x,z),true),low=query.create(ocean,'low',(x,z)=>waveFloor(x,z),true);
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    loadFloor(ocean,sample=>{waveFloor=sample;helpers.schedule('vessel-bathymetry');},()=>!disposed);
    const model=window.__gosx_scene3d_vessel_model.create(sceneState,props.vessel,physics),surface={x:0,y:0,z:0,sizeX:0,sizeZ:0,rotationY:0,slopeX:0,slopeZ:0};
    if(props.walk) {walk.surfaces=walk.surfaces||[];walk.surfaces.push(surface);}
    const mount=canvas.closest('[data-gosx-engine="GoSXScene3D"]')||canvas.parentElement;
    const read=()=>helpers.current(base.controller,sceneState.camera,sceneState._scrollCamera);
    const controller={mode:'first-person',touched:false,active:false,currentCamera:()=>state.mode==='sailing'?state.camera||read():read(),
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      syncCamera:camera=>{if(base.controller&&base.controller.syncCamera)base.controller.syncCamera(camera);},
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      applyCamera:camera=>{leave();if(base.controller&&base.controller.applyCamera)base.controller.applyCamera(camera);}};
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    function suspend(value) {
      if(base.controller&&base.controller.setSuspended)base.controller.setSuspended(value);
      else if(base.controller)base.controller.suspended=value;
    }
    function leave() {state.mode='deck';state.trim=0;state.rudder=0;controller.active=false;suspend(false);}
    function changeHelm() {
      const result=physics.helm(state,read());if(result==='far')return;
      window.__gosx_scene3d_vessel_input.clear(controls.input);
      if(result==='take') {suspend(true);controller.active=true;controller.touched=true;state.camera=physics.camera(state,read(),.1);}
      else {leave();if(base.controller&&base.controller.applyCamera)base.controller.applyCamera(Object.assign({},read(),physics.localPoint(state,state.helm.x,state.helm.y,state.helm.z),{rotationY:state.heading,rotationX:0}));}
      helpers.schedule('vessel-helm');
    }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    const controls=window.__gosx_scene3d_vessel_input.setup(canvas,mount,state,{helm:changeHelm,camera:()=>{physics.toggleCamera(state);state.camera=null;helpers.schedule('vessel-camera');},reset});
    if(base.bindReset)base.bindReset(reset);
    sceneState._gosxMotionController=controller;
    const wake=window.__gosx_scene3d_vessel_wake&&props.vessel.wake!==false?window.__gosx_scene3d_vessel_wake.create(sceneState,helpers,props.vessel):null;
    function reset() {leave();window.__gosx_scene3d_vessel_input.clear(controls.input);if(wake)wake.reset();if(base.reset)base.reset();Object.assign(state,physics.create(props.vessel,ocean,walk));first=true;lastBob=0;helpers.schedule('vessel-reset');}
  // @ts-ignore TS7006 -- plain JS method parameters are exercised without transpilation.
    return {controller,state,reset,stopInertia:()=>false,advance(dt,seconds,detail,paused) {
      if(disposed)return;
      state.reduced=!!(motion&&motion.matches);
      const camera=read(),before=new Float32Array(model.pose),aboard=state.mode==='deck'&&!first;
      const local=aboard?window.__gosx_scene3d_vessel_model.inversePoint(before,Object.assign({},camera,{y:camera.y-lastBob})):null;
      lastBob=0;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      const q=helpers.lowHardware()?low:high,sample=(x,z,t)=>query.sample(q,x,z,t);
      if(first) {state.y=sample(state.x,state.z,seconds).y;first=false;}
      if(!paused)physics.advance(state,dt,seconds,window.__gosx_scene3d_vessel_input.value(controls.input),sample,floor);
      deck(state,physics,surface);
      if(local&&Math.abs(local.x)<surface.sizeX/2+.3&&Math.abs(local.z)<surface.sizeZ/2+.3&&Math.abs(local.y-state.helm.y)<2) {
        const p=physics.localPoint(state,local.x,local.y,local.z);
        lastBob=physics.deckBob(state,seconds);p.y+=lastBob;
        if(base.controller&&base.controller.carryCamera)base.controller.carryCamera(Object.assign({},camera,p));
      }
      if(state.mode==='sailing')state.camera=physics.camera(state,camera,dt,seconds);
      model.update(state,controller.currentCamera(),seconds,detail);
      if(wake)wake.update(state,seconds,sample,detail,controller.currentCamera());
      controls.refresh(physics.canBoard(state,read()));
      mount.setAttribute('data-gosx-scene3d-vessel',state.mode);
    },dispose() {
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      disposed=true;controls.dispose();if(wake)wake.dispose();if(props.walk)walk.surfaces=walk.surfaces.filter(s=>s!==surface);
      suspend(false);base.dispose();if(sceneState._gosxMotionController===controller)sceneState._gosxMotionController=null;
    }};
  }
  window.__gosx_scene3d_vessel_api={setup};
})();
