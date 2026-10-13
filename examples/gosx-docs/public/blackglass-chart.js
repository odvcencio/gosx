// Instruments subscribe to the vessel's public state contract. The scene keeps
// ownership of motion; the server authors the chart and its survey locations.
(function () {
  "use strict";
  function mount() {
    const root=document.querySelector('.bgb');
    if(!root || root.dataset.chartMounted)return;
    root.dataset.chartMounted='true';
    const chart=root.querySelector('.bgb__chart'),journal=root.querySelector('.bgb__journal'),map=chart.querySelector('svg');
    const hold=chart.querySelector('[data-coast-hold]'),helm=chart.querySelector('[data-coast-chart-helm]'),sail=chart.querySelector('[data-coast-chart-sail]');
    const input=chart.querySelector('[data-coast-course]');
    let state=null;
    const text=(selector,value)=>{const node=chart.querySelector(selector);if(node.textContent!==value)node.textContent=value;};
    const degrees=n=>String(Math.round(n)%360).padStart(3,'0')+'°';
    function command(action,values={}) {
      const scene=root.querySelector('[data-gosx-scene3d-mounted]');
      if(!state || !scene)return;
      scene.dispatchEvent(new CustomEvent('gosx:scene3d:vessel-command',{detail:{nodeId:state.nodeId,action,...values}}));
    }
    for(const panel of [chart,journal])panel.addEventListener('toggle',()=>{
      if(!panel.open)return;
      if(document.pointerLockElement)document.exitPointerLock();
      (panel===chart?journal:chart).open=false;
    });
    const namespace='http://www.w3.org/2000/svg';
    for(const place of JSON.parse(root.dataset.coastChartPlaces)) {
      const link=document.createElementNS(namespace,'a'),dot=document.createElementNS(namespace,'circle'),label=document.createElementNS(namespace,'text');
      link.setAttribute('href','?view='+encodeURIComponent(place.view)+'&period='+encodeURIComponent(root.dataset.period));
      link.setAttribute('aria-label','Visit '+place.name);
      dot.setAttribute('cx',place.x);dot.setAttribute('cy',place.z);dot.setAttribute('r','1.2');
      label.setAttribute('x',place.x+2);label.setAttribute('y',place.z-2);label.textContent=place.name;
      link.append(dot,label);root.querySelector('[data-coast-map-places]').append(link);
      const visit=document.createElement('a');visit.href=link.getAttribute('href');visit.textContent=place.name;
      chart.querySelector('[data-coast-chart-links]').append(visit);
    }
    root.addEventListener('gosx:scene3d:vessel-state',event=>{
      const d=event.detail,p=d&&d.position;
      if(!p || ![p.x,p.z,d.bearing,d.speed,d.trim,d.distance,d.windFrom,d.windStrength].every(Number.isFinite)
        || (d.course!==null&&!Number.isFinite(d.course)) || typeof d.nodeId!=='string' || !['moored','deck','sailing'].includes(d.mode))return;
      state=d;
      map.setAttribute('viewBox',[p.x-80,p.z-60,160,120].join(' '));
      root.querySelector('[data-coast-map-ship]').setAttribute('transform',`translate(${p.x} ${p.z}) rotate(${d.bearing})`);
      text('[data-coast-bearing]',degrees(d.bearing));text('[data-coast-speed]',d.speed.toFixed(1)+' m/s');
      text('[data-coast-wind]',degrees(d.windFrom)+' · '+d.windStrength.toFixed(0)+' m/s');
      text('[data-coast-distance]',d.distance<1000?Math.round(d.distance)+' m':(d.distance/1000).toFixed(2)+' km');
      const active=d.mode==='sailing',held=d.course!==null;
      helm.disabled=false;hold.disabled=!active;sail.disabled=!active;
      helm.textContent=active?'Leave helm':'Pilot ship';sail.textContent=d.trim>.95?'Lower sail':'Full sail';hold.textContent=held?'Release course':'Hold course';
      hold.setAttribute('aria-pressed',String(held));
      text('[data-coast-chart-status]',d.grounded?'Grounded · steer toward deeper water':held?'Holding '+degrees(d.course)+' · manual steering takes over':active?'At the helm · set a course or steer freely':'Board the ship to start your voyage.');
    });
    helm.addEventListener('click',()=>command('helm'));
    sail.addEventListener('click',()=>command('trim',{trim:state.trim>.95?0:1}));
    hold.addEventListener('click',()=>{
      if(state.course!==null) {command('release-course');return;}
      const bearing=input.valueAsNumber;
      if(!Number.isFinite(bearing)||bearing<0||bearing>360) {text('[data-coast-chart-status]','Choose a course from 0 to 360 degrees.');input.focus();return;}
      command('course',{bearing});
    });
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',mount,{once:true});else mount();
})();
