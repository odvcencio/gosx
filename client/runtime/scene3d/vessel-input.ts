// Focus-scoped controls; steering and sail gestures never share a pointer ID.
(function() {
  function create() {
    return { keys:new Set(), rudderID:-1, sailID:-1, lookID:-1, lookX:0,lookY:0,origin:0, rudder:0, sail:0 };
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function down(input, role, id, x) {
    if(role==='rudder' && input.rudderID<0) { input.rudderID=id;input.origin=x;return true; }
    if(role==='sail' && input.sailID<0) { input.sailID=id;return true; }
    return false;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function move(input,id,x) { if(id===input.rudderID) {const delta=x-input.origin;input.rudder=Math.abs(delta)<=3?0:Math.max(-1,Math.min(1,delta/55));} }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function up(input,id) {
    if(id===input.rudderID) {input.rudderID=-1;input.rudder=0;}
    if(id===input.sailID) {input.sailID=-1;input.sail=0;}
    if(id===input.lookID)input.lookID=-1;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function clear(input) { input.keys.clear();input.rudderID=-1;input.sailID=-1;input.lookID=-1;input.rudder=0;input.sail=0; }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function value(input,withLook=false) {
    const result={rudder:Math.max(-1,Math.min(1,input.rudder+Number(input.keys.has('KeyD'))-Number(input.keys.has('KeyA')))),
      sail:input.sail+Number(input.keys.has('KeyW'))-Number(input.keys.has('KeyS'))};
    if(withLook)Object.assign(result,{lookYaw:Number(input.keys.has('ArrowLeft'))-Number(input.keys.has('ArrowRight')),lookPitch:Number(input.keys.has('ArrowUp'))-Number(input.keys.has('ArrowDown'))});
    return result;
  }
  // @ts-ignore TS7006 -- one scoped pointer owns look; left touch remains rudder.
  function bindLook(canvas,state,input,listen,actions) {
    const active=()=>state.mode==='sailing'&&!state.detached;
    // @ts-ignore TS7006 -- pointer events are also tested without transpilation.
    listen(canvas,'pointerdown',event=>{
      if(!active()||input.lookID>=0)return;
      if(event.pointerType==='touch'&&event.clientX<canvas.getBoundingClientRect().left+canvas.getBoundingClientRect().width/2)return;
      if(event.pointerType!=='touch'&&event.button!==0)return;
      input.lookID=event.pointerId;input.lookX=event.clientX;input.lookY=event.clientY;
      if(event.pointerType!=='touch')event.preventDefault();canvas.focus({preventScroll:true});canvas.setPointerCapture?.(event.pointerId);
    });
    // @ts-ignore TS7006 -- pointer events are also tested without transpilation.
    listen(canvas,'pointermove',event=>{
      if(!active())return;
      const locked=document.pointerLockElement===canvas;
      if(!locked&&event.pointerId!==input.lookID)return;
      const dx=locked?event.movementX:event.clientX-input.lookX,dy=locked?event.movementY:event.clientY-input.lookY;
      input.lookX=event.clientX;input.lookY=event.clientY;
      event.preventDefault();actions.look(-dx*.0028,-dy*.0028);
    });
  }
  // @ts-ignore TS7006 -- spring-centered helm shares the existing rudder authority.
  function steering(mount,canvas,state,input,listen) {
    const api=window.__gosx_scene3d_vessel_input,root=document.createElement('div'),track=document.createElement('div'),knob=document.createElement('span'),caption=document.createElement('span');
    root.className='gosx-scene3d-vessel-controls gosx-scene3d-vessel-steering';root.setAttribute('class',root.className);root.setAttribute('role','group');root.setAttribute('aria-label','Ship steering');
    root.style.cssText='position:absolute;left:14px;bottom:var(--gosx-vessel-controls-bottom,110px);z-index:12;display:none;width:144px;max-width:calc(50% - 14px);padding:8px;box-sizing:border-box;border-radius:18px;background:rgba(14,20,24,.82);color:#fff;font:12px sans-serif;touch-action:none;user-select:none;text-align:center';
    track.className='gosx-scene3d-rudder';track.setAttribute('class',track.className);track.setAttribute('role','slider');track.setAttribute('tabindex','0');track.setAttribute('aria-label','Rudder');
    for(const [name,v] of [['aria-valuemin','-100'],['aria-valuemax','100'],['aria-valuenow','0'],['aria-valuetext','Centered']])track.setAttribute(name,v);
    track.style.cssText='position:relative;height:72px;border:1px solid #aab5b080;border-radius:36px;background:linear-gradient(90deg,#edf3ed14 1px,transparent 1px) center/25% 100%;touch-action:none';
    knob.style.cssText='position:absolute;left:calc(50% - 18px);top:17px;width:36px;height:36px;border-radius:50%;background:#f0c995;box-shadow:0 2px 12px #0006;pointer-events:none';track.appendChild(knob);root.appendChild(track);
    const buttons=document.createElement('div');buttons.style.cssText='display:flex;gap:8px;margin-top:8px';root.appendChild(buttons);
    caption.textContent='Steer · right: look';caption.style.cssText='display:block;margin-top:7px;font-size:11px;color:#e4eae6';root.appendChild(caption);mount.appendChild(root);
    const active=()=>state.mode==='sailing'&&!state.detached;
    // @ts-ignore TS7006 -- scoped captured pointers preserve other held controls.
    const press=(node,event,origin)=>{if(!active()||event.button>0||!api.down(input,'rudder',event.pointerId,origin))return false;event.preventDefault();canvas.focus({preventScroll:true});node.setPointerCapture?.(event.pointerId);return true;};
    // @ts-ignore TS7006 -- track coordinates are in the same CSS pixels as canvas gestures.
    listen(track,'pointerdown',e=>{const b=track.getBoundingClientRect();if(press(track,e,b.left+b.width/2))api.move(input,e.pointerId,e.clientX);});
    // @ts-ignore TS7006 -- captured drag stays owned when the thumb leaves the pad.
    listen(track,'pointermove',e=>{if(e.pointerId===input.rudderID){e.preventDefault();api.move(input,e.pointerId,e.clientX);}});
    for(const [direction,label] of [[-1,'Steer left'],[1,'Steer right']]) {
      const b=document.createElement('button');b.type='button';b.textContent=Number(direction)<0?'←':'→';b.setAttribute('aria-label',String(label));b.setAttribute('data-gosx-rudder',String(direction));
      b.style.cssText='flex:1;min-width:48px;min-height:48px;border:1px solid #aab5b080;border-radius:12px;background:#1c2b30;color:#f0c995;font:22px sans-serif;touch-action:none';buttons.appendChild(b);
      // @ts-ignore TS7006 -- hold buttons provide a predictable gentle turn.
      listen(b,'pointerdown',e=>{if(press(b,e,e.clientX))input.rudder=Number(direction)*.7;});
      // @ts-ignore TS7006 -- keyboard users can hold the same steering targets.
      listen(b,'keydown',e=>{if(active()&&input.rudderID<0&&(e.code==='Space'||e.code==='Enter')){e.preventDefault();input.rudder=Number(direction)*.7;}});
    }
    // @ts-ignore TS7006 -- release only this pointer, never the sail or look pointer.
    for(const node of [track,...Array.from(buttons.children)])for(const event of ['pointerup','pointercancel','lostpointercapture'])listen(node,event,e=>api.up(input,e.pointerId));
    // @ts-ignore TS7006 -- a focused slider maps arrows to rudder, without camera input.
    listen(track,'keydown',e=>{if(!active()||input.rudderID>=0)return;if(['ArrowLeft','ArrowRight','Home'].includes(e.code)){e.preventDefault();input.rudder=e.code==='Home'?0:e.code==='ArrowLeft'?-.7:.7;}});
    for(const node of [track,...Array.from(buttons.children)])for(const event of ['keyup','blur'])listen(node,event,()=>{if(input.rudderID<0)input.rudder=0;});
    let shown=false,last=NaN;
    // @ts-ignore TS7006 -- refreshed from the existing Boolean helm ownership state.
    return {refresh(on) {
      if(shown!==on){shown=on;root.hidden=!on;root.style.display=on?'block':'none';}
      const n=Math.round(api.value(input).rudder*100);
      if(last!==n){last=n;knob.style.transform='translateX('+n*.4+'px)';track.setAttribute('aria-valuenow',String(n));track.setAttribute('aria-valuetext',n===0?'Centered':Math.abs(n)+'% '+(n<0?'left':'right'));}
    },dispose(){root.remove();}};
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function setup(canvas,mount,state,actions) {
    const input=create(),listeners=Array();
    const touch=window.matchMedia?.('(pointer: coarse)')?.matches;
    const helmLabels=touch?{take:'Take helm',leave:'Leave helm'}:{take:'Press E to take the helm',leave:'Leave helm · E'};
    const panel=document.createElement('div'),helm=document.createElement('button'),view=document.createElement('button');
    const readout=document.createElement('span'),raise=document.createElement('button'),lower=document.createElement('button');
    let layout='',label='';
    panel.className='gosx-scene3d-vessel-controls';panel.setAttribute('class',panel.className);
    panel.style.cssText='position:absolute;top:58px;left:50%;transform:translateX(-50%);z-index:12;display:none;grid-template-columns:1fr 1fr;gap:6px;align-items:center;padding:6px;border-radius:8px;background:rgba(14,20,24,.75);color:#fff;font:13px sans-serif';
    readout.style.cssText='grid-column:1/-1;text-align:center;white-space:normal;max-width:280px';
    // @ts-ignore TS7006 -- button arguments are evaluated as plain JavaScript
    const button=(node,label)=>{node.type='button';node.textContent=label;node.style.cssText='min-height:'+(touch?'48':'42')+'px;padding:6px 12px;border:1px solid #aab5b0;border-radius:5px;background:#1c2b30;color:#fff;touch-action:none';panel.appendChild(node);};
    button(helm,'Press E to take the helm');button(view,'Camera · V');button(lower,'Lower sail');button(raise,'Raise sail');panel.appendChild(readout);mount.appendChild(panel);
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    function listen(target,name,fn) {target.addEventListener(name,fn,{passive:false});listeners.push([target,name,fn]);}
    const pad=touch?steering(mount,canvas,state,input,listen):null;
    const disposeHUD=sceneReserveVesselHUD(state,mount,listen);
    function focused() {return document.activeElement===canvas || document.pointerLockElement===canvas;}
    bindLook(canvas,state,input,listen,actions);
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(helm,'click',()=>{actions.helm(!!state.config.quickBoard);canvas.focus({preventScroll:true});});
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(view,'click',()=>{actions.camera();canvas.focus({preventScroll:true});});
    for(const {node,amount} of [{node:raise,amount:1},{node:lower,amount:-1}]) {
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      listen(node,'pointerdown',event=>{event.preventDefault();if(down(input,'sail',event.pointerId,0)) {input.sail=amount;node.setPointerCapture?.(event.pointerId);}});
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      for(const name of ['pointerup','pointercancel','lostpointercapture'])listen(node,name,event=>up(input,event.pointerId));
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      listen(node,'keydown',event=>{if(event.code==='Space'||event.code==='Enter') {input.sail=amount;event.preventDefault();}});
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      listen(node,'keyup',()=>{input.sail=0;});
    }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(canvas,'pointerdown',event=>{
      if(state.mode!=='sailing'||state.detached||event.pointerType!=='touch')return;
      const rect=canvas.getBoundingClientRect();
      if(event.clientX-rect.left<rect.width/2 && down(input,'rudder',event.pointerId,event.clientX))canvas.setPointerCapture?.(event.pointerId);
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
      if(state.mode!=='sailing'||state.detached)return;
      if(event.code==='KeyV'&&!event.repeat) {event.preventDefault();actions.camera();}
      if(['KeyW','KeyS','KeyA','KeyD','ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(event.code)) {event.preventDefault();input.keys.add(event.code);actions.look(0,0);}
    });
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(document,'click',event=>{
      const button=event.target?.closest?.('[data-gosx-scene3d-reset]');
      if(!button)return;const id=button.getAttribute('data-gosx-scene3d-reset');
      if(id===mount.id||(id===''&&document.querySelectorAll('[data-gosx-engine="GoSXScene3D"]').length===1))actions.reset();
    });
    // @ts-ignore TS7006 -- reset events are exercised as plain JS by Node.
    listen(document,'keyup',event=>input.keys.delete(event.code));listen(window,'blur',()=>clear(input));
    listen(document,'visibilitychange',()=>{if(document.hidden)clear(input);});
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(canvas,'blur',()=>{if(!document.pointerLockElement)clear(input);});
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    listen(document,'pointerlockchange',()=>{if(!document.pointerLockElement)clear(input);});
  // @ts-ignore TS7006 -- plain JS method parameters are exercised without transpilation.
    return {input,refresh(near) {
      const active=state.mode==='sailing'&&!state.detached,hidden=!active&&!near&&!state.config.quickBoard,key=active+':'+hidden+':'+state.detached;
      if(layout!==key) {
        layout=key;panel.hidden=hidden;if(!active)clear(input);
        panel.style.display=hidden?'none':'grid';panel.style.left=active?'auto':'50%';panel.style.right=active?'14px':'auto';
        panel.style.top=active?'auto':'58px';panel.style.bottom=active?'var(--gosx-vessel-controls-bottom,110px)':'auto';panel.style.transform=active?'none':'translateX(-50%)';
        panel.style.width=active?(touch?'min(220px, calc(50% - 14px))':'280px'):'auto';panel.style.gridTemplateColumns=active?'1fr 1fr':'1fr';
        helm.textContent=active?helmLabels.leave:state.detached?'Return to helm':state.config.quickBoard?'Pilot ship':helmLabels.take;
        view.hidden=!active;raise.hidden=!active;lower.hidden=!active;readout.hidden=!active;
      }
      pad?.refresh(active);
      const next=state.grounded?'Grounded · turn toward deeper water':window.__gosx_scene3d_vessel_physics.speedCurve(state.heading-state.wind)===0?'Into the wind · '+(touch?'steer to tack':'hold A/D to tack'):Math.round(state.trim*100)+'% sail · '+state.speed.toFixed(1)+' m/s'+(touch?'':' · drag to look · A/D steer · W/S sail');
      if(label!==next) {label=next;readout.textContent=next;}
    },dispose() {disposeHUD();clear(input);panel.remove();pad?.dispose();for(const [target,name,fn] of listeners)target.removeEventListener(name,fn);} };
  }
  window.__gosx_scene3d_vessel_input={create,down,move,up,clear,value,setup};
})();
