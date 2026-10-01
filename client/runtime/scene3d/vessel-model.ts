// Model transforms and cloth geometry updates use existing scene records.
(function() {
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function matrix(state, physics, out) {
    const origin=physics.localPoint(state,0,0,0);
    for(let axis=0;axis<3;axis++) {
      const v=[0,0,0];v[axis]=1;const p=physics.localPoint(state,...v),offset=axis*4;
      out[offset]=p.x-origin.x;out[offset+1]=p.y-origin.y;out[offset+2]=p.z-origin.z;out[offset+3]=0;
    }
    out[12]=origin.x;out[13]=origin.y;out[14]=origin.z;out[15]=1;return out;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function inversePoint(transform, point) {
    const x=point.x-transform[12],y=point.y-transform[13],z=point.z-transform[14];
    return {x:transform[0]*x+transform[1]*y+transform[2]*z,y:transform[4]*x+transform[5]*y+transform[6]*z,z:transform[8]*x+transform[9]*y+transform[10]*z};
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function cloth(object, saved, state, seconds) {
    const vertices=object.vertices,base=saved.positions,uv=vertices.uvs,top=saved.top;
    const trim=state.mode==='moored'?0:state.trim,wind=Math.min(1.5,state.strength/8),phase=seconds*2.2;
    for(let i=0;i<base.length;i+=3) {
      const u=uv ? uv[i/3*2] : .5,v=uv ? uv[i/3*2+1] : .5;
      const shape=Math.sin(u*Math.PI)*Math.sin(v*Math.PI);
      // A rounded canvas bundle remains below and in front of the yard.
      const furledY=top-.12-.22*v,furledZ=saved.z+.12+.12*Math.sin(v*Math.PI);
      vertices.positions[i+1]=furledY+(base[i+1]-furledY)*trim;
      vertices.positions[i+2]=furledZ+(base[i+2]-furledZ)*trim+shape*wind*trim*(.24+.12*Math.sin(phase+u*4+base[i+1]*.7));
    }
    vertices.revision=(vertices.revision||0)+1;vertices.immutable=false;object.static=false;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function create(sceneState, config, physics) {
    const roots=[{nodeId:config.nodeId,distance:0},...(config.lods||[])],saved=new WeakMap(),pose=new Float32Array(16);
  // @ts-ignore TS7006 -- plain JS method parameters are exercised without transpilation.
    return {pose,update(state,camera,time,detail) {
      matrix(state,physics,pose);
      const distance=Math.hypot(camera.x-state.x,camera.y-state.y,camera.z-state.z);
      let root=roots[0];for(const lod of roots) if(distance>lod.distance*Math.max(.45,detail)) root=lod;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      const ready=roots.filter(r=>Array.from(sceneState.objects.keys()).some(id=>String(id).startsWith(r.nodeId+'/')));
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
      if(!ready.some(r=>r===root))root=ready[0]||root;
      for(const [id,object] of sceneState.objects) {
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
        const owner=roots.find(r=>String(id).startsWith(r.nodeId+'/'));if(!owner)continue;
        object.parentMatrix=pose;object.x=0;object.y=0;object.z=0;object.rotationX=0;object.rotationY=0;object.rotationZ=0;
        object.visible=owner===root;object._modelHidden=false;object.static=false;
        if(!object.visible)continue;
        const name=String(id);
        if(name.includes('/canvas-') && object.vertices) {
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
          if(!saved.has(object))saved.set(object,{positions:new Float32Array(object.vertices.positions),top:Math.max(...object.vertices.positions.filter((v,i)=>i%3===1)),z:Math.min(...object.vertices.positions.filter((v,i)=>i%3===2))});
          cloth(object,saved.get(object),state,time);
        }
        if(name.includes('/wind-flag')) {
          // Rotate the pennant about its own masthead pivot into world wind.
          const vertices=object.vertices;
          if(vertices) {
            if(!saved.has(object))saved.set(object,{positions:new Float32Array(vertices.positions)});
            const base=saved.get(object).positions,dir=state.wind-state.heading;
            for(let i=0;i<base.length;i+=3) {
              const reach=base[i];vertices.positions[i]=Math.sin(dir)*reach;
              vertices.positions[i+2]=Math.cos(dir)*reach;
              vertices.normals[i]=Math.cos(dir);vertices.normals[i+1]=0;vertices.normals[i+2]=-Math.sin(dir);
              vertices.positions[i+1]=base[i+1]+.12*Math.sin(time*6+reach*4)*Math.min(1,reach);
            }
            vertices.revision=(vertices.revision||0)+1;vertices.immutable=false;
          }
        }
      }
    }};
  }
  window.__gosx_scene3d_vessel_model={create,matrix,inversePoint};
})();
