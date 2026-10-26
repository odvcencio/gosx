// World-space foam fans from the stern; curved bow strips ride the same water.
(function() {
  const count=20,stride=12,bowRows=5,foamAges=8;
  // @ts-ignore TS7006 -- bounded deterministic impact spray uses existing alpha meshes.
  function spray(scene,helpers,config,physics) {
    const count=20,id='gosx-vessel-spray:'+config.nodeId,particles=Array();
    const vertices={count:count*3,positions:new Float32Array(count*9),normals:new Float32Array(count*9),uvs:new Float32Array(count*6),revision:0,dynamic:true};
    for(let i=0;i<count;i++)vertices.uvs.set([0,1,1,1,.5,0],i*6);
    helpers.addObject(scene,id,{vertices,color:'#eff4ef',materialKind:'standard',wireframe:false,roughness:1,opacity:.55,blendMode:'alpha',renderPass:'alpha',depthWrite:false,castShadow:false,receiveShadow:false,pickable:false,static:false});
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
  // @ts-ignore TS7006 -- bow strips are bounded water-following geometry.
  function bow(state,moving,positions,vertex,sample,time) {
    const cy=Math.cos(state.heading),sy=Math.sin(state.heading),cp=Math.cos(state.pitch),sp=Math.sin(state.pitch),cr=Math.cos(state.roll),sr=Math.sin(state.roll);
    const xx=cy*cr+sy*sp*sr,xz=sy*cp,zx=-sy*cr+cy*sp*sr,zz=cy*cp;
    for(let side=0;side<2;side++) {
      const sign=side===0?-1:1,start=count*stride+side*bowRows*2;
      for(let row=0;row<bowRows;row++)for(let edge=0;edge<2;edge++) {
        const t=row/(bowRows-1),width=state.beam*(.035+.075*t),x=sign*(state.beam*(.04+.64*Math.sin(t*Math.PI/2))+edge*width),z=state.length*(-.5+.44*t);
        if(moving)vertex(start+row*2+edge,state.x+xx*x+xz*z,state.z+zx*x+zz*z,edge,t/foamAges,sample,time);
        else positions.copyWithin((start+row*2+edge)*3,0,3);
      }
    }
  }
  // @ts-ignore TS7006 -- small foam patches use one water query and its local tangent.
  function foamRow(vertices,row,point,newer,state,time,sample,limit) {
    const {positions,normals,uvs}=vertices;
    const dx=newer.x-point.x,dz=newer.z-point.z,distance=Math.hypot(dx,dz);
    const co=distance>.05?-dz/distance:Math.cos(point.heading),si=distance>.05?-dx/distance:Math.sin(point.heading);
    const age=Math.max(0,Math.min(1,(time-point.time)/6)),spread=state.beam*.28+point.speed*age*6*.34;
    const cell=Math.min(foamAges-1,Math.floor(age*foamAges));
    for(let patch=0;patch<3;patch++) {
      const side=patch-1,seed=Math.sin(point.time*.7+side*7),offset=side*spread+(side?0:seed*.2);
      const x=point.x+co*offset,z=point.z-si*offset,water=sample(x,z,time),n=water.normal||{x:0,y:1,z:0};
      const nx=-n.x/Math.max(.4,n.y),nz=-n.z/Math.max(.4,n.y),width=(side?.38+.4*age:state.beam*.23*(1+.4*age))*(1+.15*seed),span=(.35+point.speed*6/limit*.52)*(1+.13*seed);
      const turn=Math.sin(seed*.18),straight=Math.cos(seed*.18);
      for(let corner=0;corner<4;corner++) {
        const across=(corner%2?1:-1)*width,along=(corner<2?-1:1)*span;
        const a=across*straight-along*turn,b=across*turn+along*straight;
        const xx=co*a+si*b,zz=-si*a+co*b,index=row*stride+patch*4+corner;
        positions[index*3]=x+xx;positions[index*3+1]=water.y+.065+nx*xx+nz*zz;positions[index*3+2]=z+zz;
        normals[index*3]=n.x;normals[index*3+1]=n.y;normals[index*3+2]=n.z;
        uvs[index*2]=side<0?1-corner%2:corner%2;uvs[index*2+1]=(cell+Number(corner>=2))/foamAges;
      }
    }
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function create(scene,helpers,config) {
    const points=count*stride+bowRows*4,id='gosx-vessel-wake:'+config.nodeId,trail=Array();
    const positions=new Float32Array(points*3),normals=new Float32Array(points*3),uvs=new Float32Array(points*2),indices=[];
    for(let i=0;i<points;i++)normals[i*3+1]=1;
    for(let i=0;i<count;i++)for(let j=0;j<3;j++) {const a=i*stride+j*4;indices.push(a,a+2,a+1,a+1,a+2,a+3);}
    for(let side=0;side<2;side++)for(let i=0;i<bowRows-1;i++) {
      const a=count*stride+side*bowRows*2+i*2;
      if(side===0)indices.push(a,a+1,a+2,a+2,a+1,a+3);
      else indices.push(a,a+2,a+1,a+2,a+3,a+1);
    }
    const vertices={count:points,positions,normals,uvs,indices:new Uint32Array(indices),revision:0,dynamic:true};
    helpers.addObject(scene,id,{vertices,color:'#edf3ed',materialKind:'standard',wireframe:false,texture:config.wakeTexture||'',roughness:1,metalness:0,
      opacity:.6,blendMode:'alpha',renderPass:'alpha',depthWrite:false,castShadow:false,receiveShadow:false,pickable:false,static:false});
    const object=scene.objects.get(id);object.vertices=vertices;let last=-Infinity;
    const physics=window.__gosx_scene3d_vessel_physics,impact=spray(scene,helpers,config,physics);
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    function vertex(index,x,z,u,v,sample,time) {
      const water=sample(x,z,time);
      positions[index*3]=x;positions[index*3+1]=water.y+.055;positions[index*3+2]=z;
      if(water.normal) {normals[index*3]=water.normal.x;normals[index*3+1]=water.normal.y;normals[index*3+2]=water.normal.z;}
      uvs[index*2]=u;uvs[index*2+1]=v;
    }
  // @ts-ignore TS7006 -- plain JS method parameters are exercised without transpilation.
    return {vertices,trail,spray:impact,update(state,time,sample,detail,camera) {
      const moving=state.speed>.35&&state.mode!=='moored',skip=detail<.6?2:1,limit=count/skip;
      const current=physics.localPoint(state,0,0,state.length*.51);
      if(moving&&time-last>6/count) {
        if(trail.length&&Math.hypot(current.x-trail[0].x,current.z-trail[0].z)>state.length*2)trail.length=0;
        trail.unshift({x:current.x,z:current.z,time,heading:state.heading,speed:state.speed});last=time;
      }
      while(trail.length>=count||(trail.length&&time-trail[trail.length-1].time>6))trail.pop();
      if(!moving&&trail.length<2) {object.visible=false;impact.update(state,time,sample,detail,camera);return;}
      const head={x:current.x,z:current.z,time,heading:state.heading,speed:state.speed};
      const rows=Math.min(limit,moving?Math.floor(trail.length/skip)+1:Math.ceil(trail.length/skip));
      for(let i=0;i<count;i++) {
        if(i&&i>=rows) {
          for(let j=0;j<stride;j++)positions.copyWithin((i*stride+j)*3,(i-1)*stride*3,(i-1)*stride*3+3);
          normals.copyWithin(i*stride*3,(i-1)*stride*3,i*stride*3);uvs.copyWithin(i*stride*2,(i-1)*stride*2,i*stride*2);continue;
        }
        const point=moving?(i?trail[i*skip-1]:head):trail[i*skip];
        const newer=i?(moving?(i===1?head:trail[(i-1)*skip-1]):trail[(i-1)*skip]):point;
        foamRow(vertices,i,point,newer,state,time,sample,limit);
      }
      bow(state,moving,positions,vertex,sample,time);
      const fade=trail.length?Math.max(0,1-(time-trail[0].time)/6):0;
      object.visible=moving||trail.length>1;object.opacity=Math.min(.72,.25+(moving?state.speed:trail[0].speed)*.065)*(moving?1:fade);vertices.revision++;
      impact.update(state,time,sample,detail,camera);
    },reset() {trail.length=0;last=-Infinity;object.visible=false;impact.reset();},dispose() {scene.objects.delete(id);trail.length=0;impact.dispose();}};
  }
  window.__gosx_scene3d_vessel_wake={create};
})();
