// A bounded transparent ribbon and two bow-wave strips, independent of ocean shaders.
(function() {
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
    const physics=window.__gosx_scene3d_vessel_physics;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    function vertex(index,x,z,u,v,sample,time) {
      positions[index*3]=x;positions[index*3+1]=sample(x,z,time).y+.055;positions[index*3+2]=z;
      uvs[index*2]=u;uvs[index*2+1]=v;
    }
  // @ts-ignore TS7006 -- plain JS method parameters are exercised without transpilation.
    return {vertices,trail,update(state,time,sample,detail) {
      const moving=state.speed>.35&&state.mode!=='moored',limit=detail<.6?14:count;
      if(moving&&time-last>.14) {
        const stern=physics.localPoint(state,0,0,state.length*.38);
        trail.unshift({x:stern.x,z:stern.z,time,heading:state.heading});last=time;
      }
      while(trail.length>limit||(trail.length&&time-trail[trail.length-1].time>6))trail.pop();
      const current=physics.localPoint(state,0,0,state.length*.38);
      for(let i=0;i<count;i++) {
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
      object.visible=moving||trail.length>1;object.opacity=Math.min(.52,.12+state.speed*.055);vertices.revision++;
    },reset() {trail.length=0;last=-Infinity;object.visible=false;},dispose() {scene.objects.delete(id);trail.length=0;}};
  }
  window.__gosx_scene3d_vessel_wake={create};
})();
