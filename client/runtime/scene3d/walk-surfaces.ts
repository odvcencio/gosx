// Decks and ramps are separate from terrain and bathymetry.
(function() {
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function height(surfaces, x, z, terrain) {
    let y=terrain;
    for(const s of surfaces || []) {
      const dx=x-s.x,dz=z-s.z,c=Math.cos(s.rotationY||0),n=Math.sin(s.rotationY||0);
      const lx=c*dx-n*dz,lz=n*dx+c*dz;
      if(Math.abs(lx)<=s.sizeX/2 && Math.abs(lz)<=s.sizeZ/2) y=Math.max(y,s.y+lx*(s.slopeX||0)+lz*(s.slopeZ||0));
    }
    return y;
  }
  window.__gosx_scene3d_walk_surfaces={height};
})();
