// Focus-scoped controls; steering and sail gestures never share a pointer ID.
(function() {
  function create() {
    return { keys:new Set(), rudderID:-1, sailID:-1, origin:0, rudder:0, sail:0 };
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function down(input, role, id, x) {
    if(role==='rudder' && input.rudderID<0) { input.rudderID=id;input.origin=x;return true; }
    if(role==='sail' && input.sailID<0) { input.sailID=id;return true; }
    return false;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function move(input,id,x) { if(id===input.rudderID) input.rudder=Math.max(-1,Math.min(1,(x-input.origin)/55)); }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function up(input,id) {
    if(id===input.rudderID) {input.rudderID=-1;input.rudder=0;}
    if(id===input.sailID) {input.sailID=-1;input.sail=0;}
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function clear(input) { input.keys.clear();input.rudderID=-1;input.sailID=-1;input.rudder=0;input.sail=0; }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function value(input) {
    return {rudder:Math.max(-1,Math.min(1,input.rudder+Number(input.keys.has('KeyD'))-Number(input.keys.has('KeyA')))),
      sail:input.sail+Number(input.keys.has('KeyW'))-Number(input.keys.has('KeyS'))};
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function setup(canvas,mount,state,actions) {
    const input=create(),listeners=Array();
    const touch=window.matchMedia&&window.matchMedia('(pointer: coarse)').matches;
    const panel=document.createElement('div'),helm=document.createElement('button'),view=document.createElement('button');
    const readout=document.createElement('span'),raise=document.createElement('button'),lower=document.createElement('button');
    const steerHint=document.createElement('span');steerHint.textContent='Drag left to steer';steerHint.hidden=true;
    steerHint.style.cssText='position:absolute;left:14px;bottom:140px;z-index:12;padding:10px;border-radius:8px;background:rgba(14,20,24,.75);color:#fff;font:13px sans-serif;pointer-events:none';mount.appendChild(steerHint);
    panel.className='gosx-scene3d-vessel-controls';panel.setAttribute('class',panel.className);
    panel.style.cssText='position:absolute;top:58px;left:50%;transform:translateX(-50%);z-index:12;display:none;grid-template-columns:1fr 1fr;gap:6px;align-items:center;padding:6px;border-radius:8px;background:rgba(14,20,24,.75);color:#fff;font:13px sans-serif';
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    readout.style.cssText='grid-column:1/-1;text-align:center;white-space:normal;max-width:280px';
    // @ts-ignore TS7006 -- button arguments are evaluated as plain JavaScript
    const button=(node,label)=>{node.type='button';node.textContent=label;node.style.cssText='min-height:42px;padding:6px 12px;border:1px solid #aab5b0;border-radius:5px;background:#1c2b30;color:#fff;touch-action:none';panel.appendChild(node);};
    button(helm,'Press E to take the helm');button(view,'Camera · V');button(lower,'Lower sail');button(raise,'Raise sail');panel.appendChild(readout);mount.appendChild(panel);
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    function listen(target,name,fn) {target.addEventListener(name,fn,{passive:false});listeners.push([target,name,fn]);}
    function focused() {return document.activeElement===canvas || document.pointerLockElement===canvas;}
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(helm,'click',()=>{actions.helm();canvas.focus({preventScroll:true});});
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(view,'click',()=>{actions.camera();canvas.focus({preventScroll:true});});
    for(const {node,amount} of [{node:raise,amount:1},{node:lower,amount:-1}]) {
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      listen(node,'pointerdown',event=>{event.preventDefault();if(down(input,'sail',event.pointerId,0)) {input.sail=amount;if(typeof node.setPointerCapture==='function')node.setPointerCapture(event.pointerId);}});
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      for(const name of ['pointerup','pointercancel','lostpointercapture'])listen(node,name,event=>up(input,event.pointerId));
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      listen(node,'keydown',event=>{if(event.code==='Space'||event.code==='Enter') {input.sail=amount;event.preventDefault();}});
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      listen(node,'keyup',()=>{input.sail=0;});
    }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(canvas,'pointerdown',event=>{
      if(state.mode!=='sailing'||event.pointerType!=='touch')return;
      const rect=canvas.getBoundingClientRect();
      if(event.clientX-rect.left<rect.width/2 && down(input,'rudder',event.pointerId,event.clientX)) {event.preventDefault();if(typeof canvas.setPointerCapture==='function')canvas.setPointerCapture(event.pointerId);}
    });
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(canvas,'pointermove',event=>{if(event.pointerId===input.rudderID) {event.preventDefault();move(input,event.pointerId,event.clientX);}});
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    for(const name of ['pointerup','pointercancel','lostpointercapture'])listen(canvas,name,event=>up(input,event.pointerId));
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(document,'keydown',event=>{
      if(!focused())return;
      if((event.code==='KeyE'||event.key==='e')&&!event.repeat) {event.preventDefault();actions.helm();}
      if(event.code==='Home') {event.preventDefault();actions.reset();return;}
      if(state.mode!=='sailing')return;
      if(event.code==='KeyV'&&!event.repeat) {event.preventDefault();actions.camera();}
      if(['KeyW','KeyS','KeyA','KeyD'].includes(event.code)) {event.preventDefault();input.keys.add(event.code);}
    });
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(document,'click',event=>{
      const button=event.target&&event.target.closest&&event.target.closest('[data-gosx-scene3d-reset]');
      if(!button)return;const id=button.getAttribute('data-gosx-scene3d-reset');
      if(id===mount.id||(id===''&&document.querySelectorAll('[data-gosx-engine="GoSXScene3D"]').length===1))actions.reset();
    });
    // @ts-ignore TS7006 -- reset events are exercised as plain JS by Node.
    listen(document,'keyup',event=>input.keys.delete(event.code));listen(window,'blur',()=>clear(input));
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(canvas,'blur',()=>{if(!document.pointerLockElement)clear(input);});
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(document,'pointerlockchange',()=>{if(!document.pointerLockElement)clear(input);});
  // @ts-ignore TS7006 -- plain JS method parameters are exercised without transpilation.
    return {input,refresh(near) {
      const active=state.mode==='sailing';panel.hidden=!active&&!near;
      steerHint.hidden=!active||!touch;
      panel.style.display=panel.hidden?'none':'grid';panel.style.left=active?'auto':'50%';panel.style.right=active?'14px':'auto';
      panel.style.top=active?'auto':'58px';panel.style.bottom=active?'110px':'auto';panel.style.transform=active?'none':'translateX(-50%)';
      panel.style.width=active?(touch?'min(220px, calc(50% - 14px))':'280px'):'auto';
      helm.textContent=active?'Leave helm · E':'Press E to take the helm';
      view.hidden=!active;raise.hidden=!active;lower.hidden=!active;readout.hidden=!active;
      readout.textContent=state.grounded?'Grounded · turn toward deeper water':window.__gosx_scene3d_vessel_physics.speedCurve(state.heading-state.wind)===0?'Into the wind · '+(touch?'steer to tack':'hold A/D to tack'):Math.round(state.trim*100)+'% sail · '+state.speed.toFixed(1)+' m/s'+(touch?'':' · A/D steer · W/S sail');
    },dispose() {clear(input);panel.remove();steerHint.remove();for(const [target,name,fn] of listeners)target.removeEventListener(name,fn);} };
  }
  window.__gosx_scene3d_vessel_input={create,down,move,up,clear,value,setup};
})();
