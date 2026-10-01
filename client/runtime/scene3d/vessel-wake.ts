// A bounded transparent ribbon and two bow-wave strips, independent of ocean shaders.
(function() {
  // @ts-ignore TS7006 -- bounded deterministic impact spray uses existing alpha meshes.
  function spray(scene,helpers,config,physics) {
    const count=20,id='gosx-vessel-spray:'+config.nodeId,particles=Array();
    const vertices={count:count*3,positions:new Float32Array(count*9),normals:new Float32Array(count*9),uvs:new Float32Array(count*6),revision:0,dynamic:true};
    for(let i=0;i<count;i++)vertices.uvs.set([0,1,1,1,.5,0],i*6);
    helpers.addObject(scene,id,{vertices,color:'#eff4ef',roughness:1,opacity:.55,blendMode:'alpha',renderPass:'alpha',depthWrite:false,castShadow:false,receiveShadow:false,pickable:false,static:false});
    const object=scene.objects.get(id);object.vertices=vertices;object.visible=false;let last=-Infinity,burst=0;
    // @ts-ignore TS7006 -- plain-JS effect entry point, tested without transpilation.
    function update(state,time,sample,detail,camera) {
      const limit=detail<.6?10:count;
      if(state.bowImpact>.2&&state.speed>.7&&!state.grounded&&time-last>.25) {
        const bow=physics.localPoint(state,0,0,-state.length*.44),water=sample(bow.x,bow.z,time);
        const co=Math.cos(state.heading),si=Math.sin(state.heading),force=Math.min(2.5,state.bowImpact);
        for(let i=0;i<8;i++) {
          const side=i%2?1:-1,seed=(Math.sin((burst*8+i+1)*12.9898)*43758.5453)%1,spread=side*(.8+Math.abs(seed)*1.5);
          particles.push({x:bow.x+co*side*state.beam*.2,y:water.y+.12,z:bow.z-si*side*state.beam*.2,time,
            vx:co*spread+state.vx*.3,vz:-si*spread+state.vz*.3,vy:1.4+force*.7+Math.abs(seed),size:.08+Math.abs(seed)*.07});
        }
        last=time;burst++;
      }
      while(particles.length>limit)particles.shift();
      for(let i=particles.length-1;i>=0;i--) {
        const p=particles[i],age=time-p.time,x=p.x+p.vx*age,z=p.z+p.vz*age,y=p.y+p.vy*age-4.905*age*age;
        if(age>1.2||y<sample(x,z,time).y)particles.splice(i,1);
      }
      if(!particles.length&&!object.visible)return;
      vertices.positions.fill(0);
      for(let i=0;i<particles.length;i++) {
        const p=particles[i],age=time-p.time,x=p.x+p.vx*age,y=p.y+p.vy*age-4.905*age*age,z=p.z+p.vz*age;
        const yaw=camera?camera.rotationY:state.heading,co=Math.cos(yaw),si=Math.sin(yaw),size=p.size*Math.max(0,1-age/1.2);
        vertices.positions.set([x-co*size,y-size,z+si*size,x+co*size,y-size,z-si*size,x,y+size*1.8,z],i*9);
        for(let j=0;j<3;j++)vertices.normals.set([si,0,co],i*9+j*3);
      }
      object.visible=particles.length>0;vertices.revision++;
    }
    return {vertices,particles,update,reset() {particles.length=0;last=-Infinity;burst=0;object.visible=false;},dispose() {scene.objects.delete(id);particles.length=0;}};
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function create(scene,helpers,config) {
    const count=24,stride=3,points=count*stride+8,id='gosx-vessel-wake:'+config.nodeId,trail=Array();
    const positions=new Float32Array(points*3),normals=new Float32Array(points*3),uvs=new Float32Array(points*2),indices=[];
    for(let i=0;i<points;i++)normals[i*3+1]=1;
    for(let i=0;i<count-1;i++)for(let j=0;j<2;j++) {const a=i*stride+j;indices.push(a,a+stride,a+1,a+1,a+stride,a+stride+1);}
    for(let i=0;i<2;i++) {
      const a=count*stride+i*4;
      if(i===0)indices.push(a,a+1,a+2,a+2,a+1,a+3);
      else indices.push(a,a+2,a+1,a+2,a+3,a+1);
    }
    const vertices={count:points,positions,normals,uvs,indices:new Uint16Array(indices),revision:0,dynamic:true};
    helpers.addObject(scene,id,{vertices,color:'#d9eee8',texture:config.wakeTexture||'',roughness:1,metalness:0,
      opacity:.4,blendMode:'alpha',renderPass:'alpha',depthWrite:false,castShadow:false,receiveShadow:false,pickable:false,static:false});
    const object=scene.objects.get(id);object.vertices=vertices;let last=-Infinity;
    const physics=window.__gosx_scene3d_vessel_physics,impact=spray(scene,helpers,config,physics);
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    function vertex(index,x,z,u,v,sample,time) {
      const water=sample(x,z,time);
      positions[index*3]=x;positions[index*3+1]=water.y+.055;positions[index*3+2]=z;
      if(water.normal)normals.set([water.normal.x,water.normal.y,water.normal.z],index*3);
      uvs[index*2]=u;uvs[index*2+1]=v;
    }
  // @ts-ignore TS7006 -- plain JS method parameters are exercised without transpilation.
    return {vertices,trail,spray:impact,update(state,time,sample,detail,camera) {
      const moving=state.speed>.35&&state.mode!=='moored',limit=detail<.6?14:count;
      if(moving&&time-last>.24) {
        const stern=physics.localPoint(state,0,0,state.length*.38);
        if(trail.length&&Math.hypot(stern.x-trail[0].x,stern.z-trail[0].z)>state.length*2)trail.length=0;
        trail.unshift({x:stern.x,z:stern.z,time,heading:state.heading});last=time;
      }
      while(trail.length>limit||(trail.length&&time-trail[trail.length-1].time>6))trail.pop();
      if(!moving&&trail.length<2) {object.visible=false;impact.update(state,time,sample,detail,camera);return;}
      const current=physics.localPoint(state,0,0,state.length*.38);
      for(let i=0;i<count;i++) {
        if(i&&i>=trail.length) {
          positions.copyWithin(i*9,(i-1)*9,i*9);normals.copyWithin(i*9,(i-1)*9,i*9);uvs.copyWithin(i*6,(i-1)*6,i*6);continue;
        }
        const point=trail[i]||trail[trail.length-1]||{x:current.x,z:current.z,time,heading:state.heading};
        const age=Math.min(1,(time-point.time)/6),width=state.beam*(.3+.7*age);
        for(let j=0;j<3;j++) {
          const side=(j-1)*width;
          vertex(i*3+j,point.x+Math.cos(point.heading)*side,point.z-Math.sin(point.heading)*side,j/2,age,sample,time);
        }
      }
      for(let side=0;side<2;side++) {
        const sign=side===0?-1:1,start=count*3+side*4;
        const corners=[[sign*.35,-state.length*.45],[sign*.85,-state.length*.45],[sign*state.beam*.8,-state.length*.25],[sign*state.beam*1.15,-state.length*.25]];
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
        corners.forEach((p,j)=>{const v=physics.localPoint(state,p[0],0,p[1]);vertex(start+j,v.x,v.z,j%2,j<2?0:.8,sample,time);});
      }
      const fade=trail.length?Math.max(0,1-(time-trail[0].time)/6):0;
      object.visible=moving||trail.length>1;object.opacity=Math.min(.52,.12+state.speed*.055)*(moving?1:fade);vertices.revision++;
      impact.update(state,time,sample,detail,camera);
    },reset() {trail.length=0;last=-Infinity;object.visible=false;impact.reset();},dispose() {scene.objects.delete(id);trail.length=0;impact.dispose();}};
  }
  window.__gosx_scene3d_vessel_wake={create};
})();
