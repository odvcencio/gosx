// The WebGPU renderer's post-effect stand-ins call these factories with
// the renderer's helper bag last; postfxBind keeps it for the modules above.
window.__gosx_scene3d_postfx_webgpu_api = {
  temporalAA: function(resources, helpers) { postfxBind(helpers); return wgpuCreateTemporalAA(resources); },
  autoExposure: function(resources, helpers) { postfxBind(helpers); return wgpuCreateAutoExposure(resources); },
  dofFragment: WGSL_POST_DOF_FRAGMENT,
  dofParams: wgpuDOFParams,
};
})();
