// Retained Scene3D transform evaluation. Demand-loaded for animated groups.
(function() {
  // Render records cross the existing dynamic Scene3D bootstrap boundary.
  type RenderRecord = Record<string, any>;
  type Matrix = ArrayLike<number>;
  type Group = RenderRecord & {id: string; parentNode: string};
  type State = {groups: Group[]; _gosxMotionRuntimeBindings?: Map<unknown, Map<unknown, {binding: {target: string; node: string; property: string}; value: number}>>};
  type GroupNode = {group: Group; world: Float64Array; local: Float64Array; prefix: Float64Array; mark: number};
  type Frame = {nodes: Map<string, GroupNode>; matrices: Map<string, Float64Array>};
  type RecordCache = {view: RenderRecord; matrix: Float64Array; transforms: Float32Array | null; skin?: RenderRecord; skinRevision?: number; streamRevision?: number; revision?: number};
  const sceneApi=window.__gosx_scene3d_api;
  const sceneNormalizeParentMatrix=sceneApi.sceneNormalizeParentMatrix;
  const sceneNormalizeDirection=sceneApi.sceneNormalizeDirection;
  // Retained transform boundaries are evaluated once on the scene clock. Both
  // renderers and picking consume the resulting bundle; geometry stays local.
  const sceneGroupFrameCaches = new WeakMap<Group[], Frame>();
  const sceneGroupRecordCaches = new WeakMap<RenderRecord, RecordCache>();
  const sceneGroupMotionPrefixes: Record<string, string> = {position:"",rotation:"rotation",scale:"scale"};
  function sceneNormalizeGroups(raw: RenderRecord[]): Group[] {
    const groups = Array.isArray(raw) ? raw : [];
    const seen = new Set();
    return groups.map(function(source: RenderRecord) {
      const parentMatrix=sceneNormalizeParentMatrix(source.parentMatrix);
      if (source.parentMatrix!=null && !parentMatrix) throw new Error("Scene3D group parent matrix is invalid");
      const group: Group = {id: String(source.id || "").trim(), parentNode: String(source.parentNode || "").trim(), parentMatrix};
      if (!group.id || seen.has(group.id)) throw new Error("Scene3D group IDs must be unique and non-empty");
      seen.add(group.id);
      for (const prefix of ["", "rotation", "spin", "scale"]) for (const axis of ["x", "y", "z"]) {
        const key = prefix ? prefix + axis.toUpperCase() : axis;
        const value = source[key] === undefined ? 0 : source[key];
        if (!Number.isFinite(value)) throw new Error("Scene3D group transform must be finite");
        group[key] = prefix === "scale" && value === 0 ? 1 : value;
      }
      return group;
    });
  }
  function sceneGroupMultiply(out: Float64Array | Float32Array, a: Matrix, b: Matrix, offset: number): boolean {
    const start = offset || 0;
    // Callers provide distinct output; instances read from their authored buffer.
    let changed=false;
    for (let c = 0; c < 4; c++) for (let r = 0; r < 4; r++) {
      const index=start+c*4+r;
      const value=a[r]*b[start+c*4] + a[4+r]*b[start+c*4+1] + a[8+r]*b[start+c*4+2] + a[12+r]*b[start+c*4+3];
      if (out[index]!==value) changed=true;
      out[index]=value;
    }
    return changed;
  }
  function sceneGroupQuaternion(x: number, y: number, z: number, intrinsic: boolean): number[] {
    const sx=Math.sin(x/2), cx=Math.cos(x/2), sy=Math.sin(y/2), cy=Math.cos(y/2), sz=Math.sin(z/2), cz=Math.cos(z/2);
    return intrinsic ? [sx*cy*cz+cx*sy*sz,cx*sy*cz-sx*cy*sz,cx*cy*sz+sx*sy*cz,cx*cy*cz-sx*sy*sz]
      : [sx*cy*cz-cx*sy*sz,cx*sy*cz+sx*cy*sz,cx*cy*sz-sx*sy*cz,cx*cy*cz+sx*sy*sz];
  }
  function sceneGroupMatrices(state: State, seconds: number): Map<string, Float64Array> {
    if (!Number.isFinite(seconds)) throw new Error("Scene3D group time must be finite");
    let frame = sceneGroupFrameCaches.get(state.groups);
    if (!frame) {
      frame = {nodes:new Map(), matrices:new Map()};
      for (const group of state.groups) {
        const node={group, world:new Float64Array(16), local:new Float64Array(16), prefix:new Float64Array(16), mark:0};
        frame.nodes.set(group.id,node); frame.matrices.set(group.id,node.world);
      }
      sceneGroupFrameCaches.set(state.groups,frame);
    }
    for (const node of frame.nodes.values()) node.mark=0;
    const layers = state._gosxMotionRuntimeBindings;
    function evaluate(node: GroupNode): Float64Array {
      if (node.mark===2) return node.world;
      if (node.mark===1) throw new Error("Scene3D group parent cycle");
      node.mark=1;
      let group=node.group, rotationBound=false;
      if (layers) {
        group=Object.assign({},group);
        for (const layer of layers.values()) for (const item of layer.values()) {
          if (item.binding.target!=="sceneNode" || item.binding.node!==group.id) continue;
          const parts=item.binding.property.split(".");
          const prefix=sceneGroupMotionPrefixes[parts[0]];
          if (prefix===undefined || !["x","y","z"].includes(parts[1])) continue;
          group[prefix ? prefix+parts[1].toUpperCase() : parts[1]]=item.value;
          if (prefix==="rotation") rotationBound=true;
        }
      }
      const a=!rotationBound && group._spinTime===seconds && group._spinQuaternion ? group._spinQuaternion : sceneGroupQuaternion(rotationBound?0:group.spinX*seconds,rotationBound?0:group.spinY*seconds,rotationBound?0:group.spinZ*seconds,true);
      const b=sceneGroupQuaternion(group.rotationX,group.rotationY,group.rotationZ,false);
      let x=a[3]*b[0]+a[0]*b[3]+a[1]*b[2]-a[2]*b[1], y=a[3]*b[1]-a[0]*b[2]+a[1]*b[3]+a[2]*b[0], z=a[3]*b[2]+a[0]*b[1]-a[1]*b[0]+a[2]*b[3], w=a[3]*b[3]-a[0]*b[0]-a[1]*b[1]-a[2]*b[2];
      const length=Math.hypot(x,y,z,w); x/=length;y/=length;z/=length;w/=length;
      const m=node.local,sx=group.scaleX,sy=group.scaleY,sz=group.scaleZ;
      m[0]=(1-2*(y*y+z*z))*sx;m[1]=2*(x*y+w*z)*sx;m[2]=2*(x*z-w*y)*sx;m[3]=0;
      m[4]=2*(x*y-w*z)*sy;m[5]=(1-2*(x*x+z*z))*sy;m[6]=2*(y*z+w*x)*sy;m[7]=0;
      m[8]=2*(x*z+w*y)*sz;m[9]=2*(y*z-w*x)*sz;m[10]=(1-2*(x*x+y*y))*sz;m[11]=0;
      m[12]=group.x;m[13]=group.y;m[14]=group.z;m[15]=1;
      if (group.parentMatrix) sceneGroupMultiply(node.prefix,group.parentMatrix,m,0);
      const local=group.parentMatrix ? node.prefix : m;
      if (group.parentNode) {
        const parent=frame.nodes.get(group.parentNode);
        if (!parent) throw new Error("Scene3D group has an unknown parent");
        sceneGroupMultiply(node.world,evaluate(parent),local,0);
      } else node.world.set(local);
      if (node.world.some(value=>!Number.isFinite(value))) throw new Error("Scene3D group produces a nonfinite matrix");
      node.mark=2;return node.world;
    }
    for (const node of frame.nodes.values()) evaluate(node);
    return frame.matrices;
  }
  function sceneGroupRecords(records: RenderRecord[], hierarchy: Map<string, Float64Array>, mode: string, batchParents = false): RenderRecord[] {
    if (!Array.isArray(records)) return records;
    let result: RenderRecord[] | null=null;
    for (let i=0;i<records.length;i++) {
      const source=records[i], parent=source && source.parentNode && hierarchy.get(source.parentNode);
      if (!parent) continue;
      // Crowds keep their local pose/motion streams resident; the renderer
      // applies this shared parent once through its existing batch uniform.
      if (batchParents && source._crowdSkin?.parent === source.parentNode) continue;
      if (!result) result=records.slice();
      let cached=sceneGroupRecordCaches.get(source);
      if (!cached) {cached={view:{},matrix:new Float64Array(16),transforms:null};sceneGroupRecordCaches.set(source,cached);}
      const view=Object.assign(cached.view,source);
      if (mode==="matrix") {
        let moved=false;
        if (source.parentMatrix) moved=sceneGroupMultiply(cached.matrix,parent,source.parentMatrix,0);
        else for(let j=0;j<16;j++) {if(cached.matrix[j]!==parent[j]) moved=true;cached.matrix[j]=parent[j];}
        view.parentMatrix=cached.matrix;view.static=false;
        if (source._crowdSkin) {
          const skin=source._crowdSkin;
          const known=cached.skin && cached.skin.atlas===skin.atlas && cached.skin.batchID===skin.batchID && cached.skin.parent===skin.parent ? cached.skin.group : null;
          if (moved || cached.skinRevision!==skin.rev) cached.revision=(cached.revision || 0)+1;
          cached.skinRevision=skin.rev;
          cached.skin=Object.assign(cached.skin || {},skin,{group:known,rev:cached.revision});
          view._crowdSkin=cached.skin;
        }
      } else if (mode==="instances") {
        const transforms=source.transforms;
        if (!cached.transforms || cached.transforms.length!==transforms.length) cached.transforms=new Float32Array(transforms.length);
        let moved=false;
        for(let j=0;j+15<transforms.length;j+=16) moved=sceneGroupMultiply(cached.transforms,parent,transforms,j) || moved;
        view.transforms=cached.transforms;
        view._cachedTransforms=null;
        if (moved || cached.streamRevision!==source._instanceStreamRevision) cached.revision=(cached.revision || 0)+1;
        cached.streamRevision=source._instanceStreamRevision;
        view._instanceStreamRevision=cached.revision;
      } else if (mode==="position") {
        view._groupParentMatrix=parent;
      } else {
        const x=source.x||0,y=source.y||0,z=source.z||0;
        view.x=parent[0]*x+parent[4]*y+parent[8]*z+parent[12];
        view.y=parent[1]*x+parent[5]*y+parent[9]*z+parent[13];
        view.z=parent[2]*x+parent[6]*y+parent[10]*z+parent[14];
        if (mode==="light") {
          const dx=source.directionX||0,dy=source.directionY||0,dz=source.directionZ||0;
          const direction=sceneNormalizeDirection({x:parent[0]*dx+parent[4]*dy+parent[8]*dz,y:parent[1]*dx+parent[5]*dy+parent[9]*dz,z:parent[2]*dx+parent[6]*dy+parent[10]*dz});
          view.directionX=direction.x;view.directionY=direction.y;view.directionZ=direction.z;
        }
      }
      result[i]=view;
    }
    return result || records;
  }


  const surfaceMatrices=new WeakMap<RenderRecord, Float64Array>();
  function groupSurface(entry: RenderRecord, point: {x: number; y: number; z: number}, surface: RenderRecord): RenderRecord {
    let matrix=surfaceMatrices.get(entry);
    if (!matrix) {matrix=new Float64Array(16);surfaceMatrices.set(entry,matrix);}
    matrix.set(entry._groupParentMatrix);
    matrix[12]=point.x;matrix[13]=point.y;matrix[14]=point.z;
    surface.parentMatrix=matrix;
    surface.x=surface.y=surface.z=0;
    surface.shiftX=surface.shiftY=surface.shiftZ=0;
    return surface;
  }
  // GenSpin is a delta quaternion. Keep the authored orientation and sample
  // it exactly once, including when the shared Go motion runtime owns time.
  function groupSpin(state: State, ref: string, seconds: number, values: Matrix, offset: number): boolean {
    const group=state.groups?.find(group=>group.id===ref);
    if (!group) return false;
    if (!group._spinProgramInvalidated) {
      group._spinQuaternion=[values[offset],values[offset+1],values[offset+2],values[offset+3]];
      group._spinTime=seconds;
    }
    return true;
  }
  function patchGroup(state: State, objectID: string, data: RenderRecord): boolean {
    const group=state.groups?.find(group=>group.id===String(objectID));
    if (!group) return false;
    const next=sceneNormalizeGroups([Object.assign({},group,data,{id:group.id})])[0];
    sceneGroupMatrices({groups:state.groups.map(item=>item===group?next:item)},0);
    Object.assign(group,next);
    if (["spinX","spinY","spinZ"].some(key=>Object.prototype.hasOwnProperty.call(data,key))) {
      group._spinProgramInvalidated=true;delete group._spinQuaternion;delete group._spinTime;
    }
    return true;
  }
  window.__gosx_scene3d_hierarchy_api={normalize:sceneNormalizeGroups,matrices:sceneGroupMatrices,records:sceneGroupRecords,patch:patchGroup,surface:groupSurface,spin:groupSpin};
})();
