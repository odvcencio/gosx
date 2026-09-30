// Local fixed-step arcade sailing. Every public helper is plain JavaScript.
(function() {
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
  const radians = Math.PI / 180;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  const angle = v => Math.atan2(Math.sin(v), Math.cos(v));
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function speedCurve(offWind) {
    const a = Math.abs(angle(offWind));
    if (a <= 40 * radians) return 0;
    if (a < 90 * radians) return 0.85 * Math.sin((a - 40 * radians) / (50 * radians) * Math.PI / 2);
    return 0.85 + 0.15 * Math.sin((a - 90 * radians) / (90 * radians) * Math.PI);
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function create(config, ocean, collision) {
    const c = config, p = c.position || {}, helm = c.helm || {};
    return { config: c, collision: collision || {}, ocean: ocean || {}, x: p.x || 0, y: p.y || 0, z: p.z || 0,
      heading: c.heading || 0, pitch: 0, roll: 0, vy: 0, vp: 0, vr: 0, vx: 0, vz: 0, yawVelocity: 0,
      length: c.length || 22, beam: c.beam || 5, draft: c.draft || 1.4, deck: c.deckHeight || 2.5,
      helm: { x: helm.x || 0, y: helm.y || 4.2, z: helm.z || 6.5 }, trim: c.sailTrim == null ? .55 : c.sailTrim, rudder: 0,
      wind: (c.windDirection == null ? (ocean.windDirection || 0) : c.windDirection) * radians,
      strength: c.windStrength || 8, maxSpeed: c.maxSpeed || 10, mode: "moored", cameraMode: "stern",
      grounded: false, accumulator: 0, speed: 0, tack: 0, cameraReady: false, camera: {x:0,y:0,z:0,rotationX:0,rotationY:0,rotationZ:0} };
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function localPoint(s, x, y, z) {
    const cp = Math.cos(s.pitch), sp = Math.sin(s.pitch), cr = Math.cos(s.roll), sr = Math.sin(s.roll);
    const cy = Math.cos(s.heading), sy = Math.sin(s.heading);
    const xx = cr*x-sr*y, yy = sr*x+cr*y;
    const zz = sp*yy+cp*z, y2 = cp*yy-sp*z;
    return { x: s.x+cy*xx+sy*zz, y: s.y+y2, z: s.z-sy*xx+cy*zz };
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function canBoard(s, camera) {
    const h = localPoint(s,s.helm.x,s.helm.y,s.helm.z);
    return Math.hypot(camera.x-h.x,camera.y-h.y,camera.z-h.z) <= (s.config.boardRadius || 4);
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function helm(s, camera) {
    if (s.mode === "sailing") { s.mode = "deck"; s.trim = 0; s.rudder = 0; return "leave"; }
    if (!canBoard(s,camera)) return "far";
    s.mode = "sailing"; s.rudder = 0; return "take";
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function toggleCamera(s) { s.cameraMode = s.cameraMode === "stern" ? "wheel" : "stern"; return s.cameraMode; }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function spring(s, key, velocity, goal, dt, frequency) {
    s[velocity] += ((goal-s[key])*frequency*frequency-2*frequency*s[velocity])*dt;
    s[key] += s[velocity]*dt;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function buoyancy(s, dt, time, sample, floor) {
    const l = s.length*.35, b = s.beam*.4;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
    const water = (x,z) => { const p=localPoint(s,x,0,z); return sample(p.x,p.z,time).y; };
    const bow=water(0,-l), stern=water(0,l), port=water(-b,0), starboard=water(b,0);
    const off = angle(s.heading-s.wind), heel = Math.sin(off)*s.trim*s.speed*.009;
    const base = (bow+stern+port+starboard)/4;
    let bottom = floor(s.x,s.z);
    for(const along of [-s.length*.36,s.length*.36]) {
      const p=localPoint(s,0,0,along);bottom=Math.max(bottom,floor(p.x,p.z));
    }
    s.grounded = bottom > base-s.draft;
    spring(s,"y","vy",Math.max(base,bottom+s.draft),dt,3.8);
    spring(s,"pitch","vp",clamp(Math.atan2(bow-stern,2*l),-.22,.22),dt,3.2);
    spring(s,"roll","vr",clamp(Math.atan2(starboard-port,2*b)+heel,-.26,.26),dt,3.2);
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function contains(c, x, z, radius, level, draft) {
    const dx=x-(c.x||0), dz=z-(c.z||0), y=c.y||0;
    if (c.kind === "box") {
      if (c.sizeY > 0 && (y+c.sizeY/2 < level-draft || y-c.sizeY/2 > level+1)) return false;
      const co=Math.cos(c.rotationY||0), si=Math.sin(c.rotationY||0);
      const lx=co*dx-si*dz, lz=si*dx+co*dz;
      return Math.hypot(Math.max(0,Math.abs(lx)-(c.sizeX||0)/2),Math.max(0,Math.abs(lz)-(c.sizeZ||0)/2)) < radius;
    }
    if (c.kind === "sphere") {
      if (y+(c.radius||0)<level-draft || y-(c.radius||0)>level+1) return false;
    } else if (c.height>0 && (y+c.height<level-draft || y>level+1)) return false;
    return Math.hypot(dx,dz) < radius+(c.radius||0);
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function allowed(s, x, z, heading, floor) {
    const co=Math.cos(heading),si=Math.sin(heading), radius=s.beam*.38;
    for (const along of [-s.length*.36,0,s.length*.36]) {
      const px=x+si*along,pz=z+co*along,b=s.config.bounds || s.collision.bounds;
      if (b && (px-radius<b.minX || px+radius>b.maxX || pz-radius<b.minZ || pz+radius>b.maxZ)) return false;
      if (floor(px,pz) > (s.ocean.level||0)-s.draft+.15) {
        // A grounded hull can turn or retreat across equal/deeper seabed.
        // Refusing every shallow candidate would trap it after a low swell.
        const oldX=s.x+Math.sin(s.heading)*along,oldZ=s.z+Math.cos(s.heading)*along;
        if(!s.grounded || floor(px,pz)>floor(oldX,oldZ)+.001)return false;
      }
      for (const c of s.collision.colliders || []) if (contains(c,px,pz,radius,s.ocean.level||0,s.draft)) return false;
    }
    return true;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function step(s, dt, time, input, sample, floor) {
    buoyancy(s,dt,time,sample,floor);
    if (s.mode === "moored") return;
    const sailing = s.mode === "sailing";
    s.trim=clamp(s.trim+(sailing ? (input.sail||0)*dt*.4 : -dt*.8),0,1);
    s.rudder += ((sailing ? clamp(input.rudder||0,-1,1) : 0)-s.rudder)*(1-Math.exp(-dt*7));
    const off=angle(s.heading-s.wind), curve=speedCurve(off), fx=-Math.sin(s.heading),fz=-Math.cos(s.heading);
    const target=s.maxSpeed*Math.min(1.5,s.strength/8)*curve*s.trim*(s.grounded ? .03 : 1);
    const forward=s.vx*fx+s.vz*fz, side=s.vx*fz-s.vz*fx;
    const drive=(target-forward)*.6, drag=side*1.4;
    const drift=s.trim*s.strength*.025*Math.abs(Math.sin(off));
    s.vx+=(drive*fx-drag*fz+Math.sin(s.wind)*drift)*dt;
    s.vz+=(drive*fz+drag*fx+Math.cos(s.wind)*drift)*dt;
    if(s.grounded) {const friction=Math.exp(-dt*6);s.vx*=friction;s.vz*=friction;}
    s.speed=Math.hypot(s.vx,s.vz);
    const turn=-s.rudder*(.1+Math.min(s.speed,10)*.035);
    s.yawVelocity+=(turn-s.yawVelocity)*(1-Math.exp(-dt*3.5));
    const heading=angle(s.heading+s.yawVelocity*dt);
    if (allowed(s,s.x,s.z,heading,floor)) s.heading=heading; else s.yawVelocity*=.3;
    const nx=s.x+s.vx*dt,nz=s.z+s.vz*dt;
    if (allowed(s,nx,nz,s.heading,floor)) { s.x=nx;s.z=nz; }
    else {
      if (allowed(s,nx,s.z,s.heading,floor)) s.x=nx; else s.vx*=Math.exp(-dt*12);
      if (allowed(s,s.x,nz,s.heading,floor)) s.z=nz; else s.vz*=Math.exp(-dt*12);
      s.trim=Math.max(0,s.trim-dt*.1);
    }
    s.tack=Math.sign(Math.sin(off));
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function advance(s, dt, time, input, sample, floor) {
    s.accumulator+=Math.max(0,Math.min(.1,dt));
    const tick=1/60;
    while(s.accumulator>=tick-1e-9) { s.accumulator-=tick;step(s,tick,time-s.accumulator,input,sample,floor); }
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function camera(s, previous, dt) {
    const wheel=s.cameraMode === "wheel", h=s.helm;
    const p=localPoint(s,wheel?h.x:0,wheel?h.y:7,wheel?h.z:s.length*.78);
    const target=localPoint(s,0,wheel?h.y:2.5,wheel?-s.length: -s.length*.15);
    const yaw=Math.atan2(-(target.x-p.x),-(target.z-p.z)), pitch=Math.atan2(target.y-p.y,Math.hypot(target.x-p.x,target.z-p.z));
    const result=Object.assign({},previous,{x:p.x,y:p.y,z:p.z,rotationX:pitch,rotationY:yaw,rotationZ:wheel?s.roll*.35:0});
    if (!wheel && s.cameraReady && s.camera) {
      const alpha=1-Math.exp(-dt*5);
      for(const key of ['x','y','z','rotationX']) result[key]=s.camera[key]+(result[key]-s.camera[key])*alpha;
      result.rotationY=s.camera.rotationY+angle(yaw-s.camera.rotationY)*alpha;
    }
    s.camera=result;s.cameraReady=true;return result;
  }
  window.__gosx_scene3d_vessel_physics={create,speedCurve,localPoint,canBoard,helm,toggleCamera,buoyancy,allowed,step,advance,camera};
})();
