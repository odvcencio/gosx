const SCENE_CROWD_RECORD_BYTES=32,SCENE_CROWD_BLOCK_SEATS=128,SCENE_CROWD_MAX_SEATS=2e5,SCENE_CROWD_MAX_SECTIONS=1024,SCENE_CROWD_THRESHOLD_ONE=16777216,SCENE_CROWD_ROLL={fill:1,color:2,accent:3,prop:4,variant:5,phase:6,eager:7,yaw:8,jitterX:9,jitterZ:10};function sceneCrowdHash(e){let t=e>>>0;return t^=t>>>16,t=Math.imul(t,2146121005)>>>0,t^=t>>>15,t=Math.imul(t,2221713035)>>>0,t^=t>>>16,t>>>0}function sceneCrowdRoll(e,t){return sceneCrowdHash((e^sceneCrowdHash(t))>>>0)>>>8}function sceneCrowdPieceAt(e,t,o,n){if(!e.corner){n[0]=e.px+e.tx*t+e.nx*o,n[1]=e.pz+e.tz*t+e.nz*o,n[2]=e.nx,n[3]=e.nz;return}const a=e.angle+e.sign*(t/e.radius),r=Math.cos(a),i=Math.sin(a),c=e.radius+o;n[0]=e.cx+c*r,n[1]=e.cz+c*i,n[2]=r,n[3]=i}function sceneCrowdPieceFactor(e,t){return e.corner?(e.radius+t)/e.radius:1}function sceneCrowdPieceSplit(e,t,o){const n=Object.assign({},e);return n.length=o-t,e.corner?n.angle=e.angle+e.sign*(t/e.radius):(n.px=e.px+e.tx*t,n.pz=e.pz+e.tz*t),n}function sceneCrowdNewPiece(e){return Object.assign({corner:!1,base:0,length:0,px:0,pz:0,tx:0,tz:0,nx:0,nz:0,cx:0,cz:0,radius:0,angle:0,sign:1},e)}function sceneCrowdBowlLoop(e){const t=e.halfWidth,o=e.halfDepth,n=e.corner,a=t-n,r=o-n,i=Math.PI/2,c=e.centerX||0,s=e.centerZ||0,l=[sceneCrowdNewPiece({px:c+t,pz:s,tx:0,tz:1,nx:1,nz:0,length:r}),sceneCrowdNewPiece({corner:!0,cx:c+a,cz:s+r,radius:n,angle:0,sign:1,length:i*n}),sceneCrowdNewPiece({px:c+a,pz:s+o,tx:-1,tz:0,nx:0,nz:1,length:2*a}),sceneCrowdNewPiece({corner:!0,cx:c-a,cz:s+r,radius:n,angle:i,sign:1,length:i*n}),sceneCrowdNewPiece({px:c-t,pz:s+r,tx:0,tz:-1,nx:-1,nz:0,length:2*r}),sceneCrowdNewPiece({corner:!0,cx:c-a,cz:s-r,radius:n,angle:Math.PI,sign:1,length:i*n}),sceneCrowdNewPiece({px:c-a,pz:s-o,tx:1,tz:0,nx:0,nz:-1,length:2*a}),sceneCrowdNewPiece({corner:!0,cx:c+a,cz:s-r,radius:n,angle:3*i,sign:1,length:i*n}),sceneCrowdNewPiece({px:c+t,pz:s-r,tx:0,tz:1,nx:1,nz:0,length:r})],f=[];let u=0;for(const d of l)d.length<=1e-9||(d.base=u,u+=d.length,f.push(d));return{pieces:f,total:u}}function sceneCrowdPath(e){if(e.shape==="line"){const s=e.x0||0,l=e.z0||0,f=(e.x1||0)-s,u=(e.z1||0)-l,d=Math.sqrt(f*f+u*u),x=f/d,h=u/d;let v=-h,_=x;const p=s+f/2-(e.facingX||0),w=l+u/2-(e.facingZ||0);return v*p+_*w<0&&(v=-v,_=-_),{pieces:[sceneCrowdNewPiece({px:s,pz:l,tx:x,tz:h,nx:v,nz:_,length:d})],length:d,closed:!1}}if(e.shape==="arc"){const s=e.startAngle||0,l=(e.endAngle||0)-s,f=Math.abs(l)*e.radius;return{pieces:[sceneCrowdNewPiece({corner:!0,cx:e.centerX||0,cz:e.centerZ||0,radius:e.radius,angle:s,sign:l<0?-1:1,length:f})],length:f,closed:!1}}const t=sceneCrowdBowlLoop(e),o=e.loopStart||0,n=e.loopEnd||0;if(o===n)return{pieces:t.pieces,length:t.total,closed:!0};const a=o*t.total;let r=n*t.total;r<=a&&(r+=t.total);const i=[];let c=0;for(let s=0;s<2;s++){const l=s*t.total;for(const f of t.pieces){const u=f.base+l,d=u+f.length,x=Math.max(u,a),h=Math.min(d,r);if(h-x<=1e-9)continue;const v=sceneCrowdPieceSplit(f,x-u,h-u);v.base=c,c+=v.length,i.push(v)}}return{pieces:i,length:c,closed:!1}}function sceneCrowdPathPoint(e,t,o,n,a,r){let i=null,c=0;for(const s of e){const l=Math.max(s.base,t),f=Math.min(s.base+s.length,o);if(f<=l)continue;const u=sceneCrowdPieceFactor(s,a),d=(f-l)*u;if(i=s,c=f-s.base,n<=d){sceneCrowdPieceAt(s,l-s.base+n/u,a,r);return}n-=d}i&&sceneCrowdPieceAt(i,c,a,r)}function sceneCrowdSpans(e,t){let o=0;for(let r=0;r<t.sectionCount;r++)o+=e.sections[t.firstSection+r].span;const n=[0];let a=0;for(let r=0;r<t.sectionCount;r++)a+=e.sections[t.firstSection+r].span,n.push(a/o);return n[t.sectionCount]=1,n}function sceneCrowdSlot(e,t){for(let o=0;o<t.length;o++)if(e<t[o])return o;return 0}function sceneCrowdPropFor(e,t){for(let o=0;o<4;o++)if(e<t[o])return o+1;return 0}function sceneCrowdLayout(e,t){const o={index:0,seat:0,section:0,stand:0,row:0,x:0,y:0,z:0,yaw:0,wave:0,seed:0,color:0,accent:0,prop:0,variant:0,phase:0,eagerness:0},n=new Float64Array(4),a=(e.seed||0)>>>0;let r=0,i=0,c=!1;function s(){const u=sceneCrowdHash(a+Math.imul(r+1,2654435769)>>>0);return r++,u}function l(u,d,x,h,v,_,p,w){const y=e.sections[x];if(sceneCrowdRoll(u,SCENE_CROWD_ROLL.fill)>=y.fill)return;if(i>=2e5){c=!0;return}w+=(sceneCrowdRoll(u,SCENE_CROWD_ROLL.yaw)/16777216-.5)*.14;let E=Math.atan2(p,v)/(2*Math.PI);E<0&&(E+=1),E>=1&&(E=0),o.index=i,o.seat=r-1,o.section=x,o.stand=d,o.row=h,o.x=v,o.y=_,o.z=p,o.yaw=w,o.wave=E,o.seed=u,o.color=sceneCrowdSlot(sceneCrowdRoll(u,SCENE_CROWD_ROLL.color),y.colorThresholds),o.accent=sceneCrowdSlot(sceneCrowdRoll(u,SCENE_CROWD_ROLL.accent),y.accentThresholds),o.prop=sceneCrowdPropFor(sceneCrowdRoll(u,SCENE_CROWD_ROLL.prop),y.propThresholds),o.variant=sceneCrowdRoll(u,SCENE_CROWD_ROLL.variant)>>>16,o.phase=sceneCrowdRoll(u,SCENE_CROWD_ROLL.phase)>>>8,o.eagerness=sceneCrowdRoll(u,SCENE_CROWD_ROLL.eager)>>>16,i++,t(o)}const f=Array.isArray(e.stands)?e.stands:[];for(let u=0;u<f.length&&!c;u++){const d=f[u],x=sceneCrowdSpans(e,d);if(d.shape==="floor"){const p=1/Math.sqrt(d.density),w=Math.floor(2*d.halfWidth/p+1e-9),y=Math.floor(2*d.halfDepth/p+1e-9);if(w<1||y<1)continue;const E=(d.centerX||0)-d.halfWidth+(2*d.halfWidth-w*p)/2,m=(d.centerZ||0)-d.halfDepth+(2*d.halfDepth-y*p)/2;for(let C=0;C<d.sectionCount;C++){const b=Math.floor(x[C]*w+.5),S=Math.floor(x[C+1]*w+.5);for(let g=0;g<y;g++)for(let A=b;A<S;A++){const P=s(),M=(sceneCrowdRoll(P,SCENE_CROWD_ROLL.jitterX)/16777216-.5)*.6,T=(sceneCrowdRoll(P,SCENE_CROWD_ROLL.jitterZ)/16777216-.5)*.6,R=E+(A+.5+M)*p,U=m+(g+.5+T)*p;l(P,u,d.firstSection+C,g,R,d.frontHeight||0,U,Math.atan2((d.facingX||0)-R,(d.facingZ||0)-U))}}continue}const h=sceneCrowdPath(d),v=d.aisleWidth/2,_=d.seatWidth;for(let p=0;p<d.sectionCount;p++){const w=d.firstSection+p,y=x[p]*h.length,E=x[p+1]*h.length,m=!h.closed&&p===0?0:v,C=!h.closed&&p===d.sectionCount-1?0:v;for(let b=0;b<d.rows;b++){const S=(b+.5)*d.rowDepth,g=d.frontHeight+b*d.rowRise;let A=0;for(const R of h.pieces){const U=Math.max(R.base,y),N=Math.min(R.base+R.length,E);N>U&&(A+=(N-U)*sceneCrowdPieceFactor(R,S))}const P=A-m-C,M=Math.floor(P/_+1e-9);if(M<1)continue;const T=m+(P-M*_)/2;for(let R=0;R<M;R++)sceneCrowdPathPoint(h.pieces,y,E,T+(R+.5)*_,S,n),l(s(),u,w,b,n[0],g,n[1],Math.atan2(-n[2],-n[3]))}}}return c?-1:i}function sceneCrowdPack(e){const t=Array.isArray(e.sections)?e.sections.length:0;if(t<1||t>1024)return{error:"crowd has "+t+" sections"};const o=Math.max(0,Math.min(2e5,e.seats|0));let n=Math.max(1,o),a=new ArrayBuffer(n*32),r=new Float32Array(a),i=new Uint16Array(a),c=new Uint8Array(a);const s=[],l=[],f=new Uint32Array(t);let u=0,d=-1,x=0,h=0,v=0,_=0,p=0,w=0;function y(m){if(d<0||m<=u)return;s.push(u,m-u,d);const C=(x+_)/2,b=(h+p)/2,S=(v+w)/2;l.push(C,b+.9,S,Math.hypot(_-C,p-b,w-S)+1.4)}const E=sceneCrowdLayout(e,function(m){const C=m.index;if(C>=n){n*=2;const g=new ArrayBuffer(n*32);new Uint8Array(g).set(c),a=g,r=new Float32Array(a),i=new Uint16Array(a),c=new Uint8Array(a)}m.section!==d||C-u>=128?(y(C),u=C,d=m.section,x=_=m.x,h=p=m.y,v=w=m.z):(m.x<x&&(x=m.x),m.x>_&&(_=m.x),m.y<h&&(h=m.y),m.y>p&&(p=m.y),m.z<v&&(v=m.z),m.z>w&&(w=m.z));const b=C*8;r[b]=m.x,r[b+1]=m.y,r[b+2]=m.z,r[b+3]=m.yaw,r[b+4]=m.wave,i[b*2+10]=m.section,i[b*2+11]=m.phase;const S=C*32;c[S+24]=m.color,c[S+25]=m.accent,c[S+26]=m.prop,c[S+27]=m.variant,c[S+28]=m.eagerness,c[S+29]=0,c[S+30]=0,c[S+31]=0,f[m.section]++});return E<0?{error:"crowd passes 200000 seats"}:(y(E),{count:E,records:new Uint8Array(a,0,E*32),blocks:new Uint32Array(s),spheres:new Float32Array(l),sectionFans:f,mismatch:o!==E})}const SCENE_CROWD_JOINTS=11,SCENE_CROWD_SEGMENTS=24,SCENE_CROWD_VERTEX_BYTES=36,SCENE_CROWD_ACTIONS=[["idle",4,-1],["stand",3,-1],["cheer",.8,.25],["clap",.5,.5],["wave-arms",1.6,-1],["jump",.7,.92],["twirl",.6,-1],["hold-up",2.4,-1],["wave",1,.5]],SCENE_CROWD_WAVE_ACTION=8,SCENE_CROWD_PARENTS=[-1,0,1,1,3,1,5,0,7,0,9],SCENE_CROWD_PIVOTS=[[0,.95,0],[0,.97,0],[0,1.47,0],[.22,1.42,0],[.22,1.13,0],[-.22,1.42,0],[-.22,1.13,0],[.1,.92,0],[.1,.49,0],[-.1,.92,0],[-.1,.49,0]];function sceneCrowdZeroPose(){const e=[];for(let t=0;t<SCENE_CROWD_JOINTS;t++)e.push([0,0,0]);return{root:[0,0,0],angles:e}}function sceneCrowdArms(e,t,o,n,a,r,i){e.angles[3]=[t,0,o],e.angles[4]=[n,0,0],e.angles[5]=[a,0,-r],e.angles[6]=[i,0,0]}function sceneCrowdSit(e){e.root=[0,-.45,-.06],e.angles[7]=[-1.5,0,.04],e.angles[8]=[1.5,0,0],e.angles[9]=[-1.5,0,-.04],e.angles[10]=[1.5,0,0],sceneCrowdArms(e,-.45,.05,-1,-.45,.05,-1)}const SCENE_CROWD_TAU=Math.PI*2;function sceneCrowdActionPose(e,t){const o=sceneCrowdZeroPose(),n=Math.sin(SCENE_CROWD_TAU*t),a=Math.cos(SCENE_CROWD_TAU*t);switch(e){case 0:sceneCrowdSit(o),o.angles[1]=[.02*n,0,.03*Math.sin(SCENE_CROWD_TAU*2*t)],o.angles[2]=[.05*a,.3*n,0];break;case 1:o.root=[.025*n,0,0],o.angles[1]=[0,0,-.04*n],o.angles[2]=[.08*Math.sin(SCENE_CROWD_TAU*2*t),.15*a,0],sceneCrowdArms(o,-.12,.08,-.15,-.12,.08,-.15);break;case 2:{const r=.5+.5*a;o.root=[0,.05*r,0],o.angles[1]=[-.1,0,0],o.angles[2]=[-.25,0,0],sceneCrowdArms(o,-2.55-.35*r,.3,-.9+.8*r,-2.55-.35*r,.3,-.9+.8*r);break}case 3:{const r=.5+.5*a;o.angles[1]=[.05,0,0],sceneCrowdArms(o,-.95,-.15+.5*r,-1,-.95,-.15+.5*r,-1);break}case 4:o.angles[1]=[0,0,.1*n],o.root=[.03*n,0,0],sceneCrowdArms(o,-2.9,.15+.45*n,-.2,-2.9,.15-.45*n,-.2);break;case 5:{let r=0,i=0;t<.15?i=Math.sin(Math.PI*t/.15):t<.92?r=Math.sin(Math.PI*(t-.15)/.77):i=Math.sin(Math.PI*(t-.92)/.08),o.root=[0,.32*r-.14*i,0],o.angles[7]=[-.7*i,0,.03],o.angles[8]=[1.3*i,0,0],o.angles[9]=[-.7*i,0,-.03],o.angles[10]=[1.3*i,0,0],o.angles[1]=[.35*i,0,0],sceneCrowdArms(o,-.6-1.9*r,.25,-.3,-.6-1.9*r,.25,-.3);break}case 6:o.angles[1]=[0,.1*n,0],sceneCrowdArms(o,-.15,.1,-.2,-2.75+.3*a,.35*n,-.25);break;case 7:o.root=[.03*n,0,0],o.angles[1]=[0,0,.05*n],sceneCrowdArms(o,-2.65,.42,-.25,-2.65,.42,-.25);break;default:{const r=Math.sin(Math.PI*Math.min(1,Math.max(0,t))),i=r*r*(3-2*r),c=sceneCrowdZeroPose();sceneCrowdSit(c);const s=sceneCrowdZeroPose();sceneCrowdArms(s,-2.95,.25,-.1,-2.95,.25,-.1),s.angles[2]=[-.2,0,0];for(let l=0;l<3;l++)o.root[l]=c.root[l]+(s.root[l]-c.root[l])*i;for(let l=0;l<SCENE_CROWD_JOINTS;l++)for(let f=0;f<3;f++)o.angles[l][f]=c.angles[l][f]+(s.angles[l][f]-c.angles[l][f])*i}}return o}function sceneCrowdMat(){const e=new Float64Array(16);return e[0]=e[5]=e[10]=e[15]=1,e}function sceneCrowdMul(e,t){const o=new Float64Array(16);for(let n=0;n<4;n++)for(let a=0;a<4;a++)o[n*4+a]=e[a]*t[n*4]+e[4+a]*t[n*4+1]+e[8+a]*t[n*4+2]+e[12+a]*t[n*4+3];return o}function sceneCrowdTRS(e,t,o,n){const a=Math.cos(n[0]),r=Math.sin(n[0]),i=Math.cos(n[1]),c=Math.sin(n[1]),s=Math.cos(n[2]),l=Math.sin(n[2]),f=sceneCrowdMat();return f[0]=i*s,f[1]=i*l,f[2]=-c,f[4]=r*c*s-a*l,f[5]=r*c*l+a*s,f[6]=r*i,f[8]=a*c*s+r*l,f[9]=a*c*l-r*s,f[10]=a*i,f[12]=e,f[13]=t,f[14]=o,f}function sceneCrowdSkinMatrices(e,t){const o=[],n=t/1.75;for(let a=0;a<SCENE_CROWD_JOINTS;a++){const r=SCENE_CROWD_PIVOTS[a],i=SCENE_CROWD_PARENTS[a];let c;if(i<0)c=sceneCrowdTRS((r[0]+e.root[0])*n,(r[1]+e.root[1])*n,(r[2]+e.root[2])*n,e.angles[a]);else{const s=SCENE_CROWD_PIVOTS[i];c=sceneCrowdTRS((r[0]-s[0])*n,(r[1]-s[1])*n,(r[2]-s[2])*n,e.angles[a])}o.push(i<0?c:sceneCrowdMul(o[i],c))}return o.map(function(a,r){const i=sceneCrowdMat();return i[12]=-SCENE_CROWD_PIVOTS[r][0]*n,i[13]=-SCENE_CROWD_PIVOTS[r][1]*n,i[14]=-SCENE_CROWD_PIVOTS[r][2]*n,sceneCrowdMul(a,i)})}function sceneCrowdBake(e){const t=SCENE_CROWD_JOINTS*4,o=1+SCENE_CROWD_ACTIONS.length*(SCENE_CROWD_SEGMENTS+1),n=new Float32Array(t*o*4),a=new Float32Array(SCENE_CROWD_ACTIONS.length*4);function r(i,c){for(let s=0;s<SCENE_CROWD_JOINTS;s++)n.set(c[s],(i*t+s*4)*4)}r(0,sceneCrowdSkinMatrices(sceneCrowdZeroPose(),e));for(let i=0;i<SCENE_CROWD_ACTIONS.length;i++){const c=1+i*(SCENE_CROWD_SEGMENTS+1);a[i*4]=c,a[i*4+1]=SCENE_CROWD_SEGMENTS,a[i*4+2]=SCENE_CROWD_ACTIONS[i][1],a[i*4+3]=SCENE_CROWD_ACTIONS[i][2];for(let s=0;s<=SCENE_CROWD_SEGMENTS;s++){const l=i===SCENE_CROWD_WAVE_ACTION?s/SCENE_CROWD_SEGMENTS:s%SCENE_CROWD_SEGMENTS/SCENE_CROWD_SEGMENTS;r(c+s,sceneCrowdSkinMatrices(sceneCrowdActionPose(i,l),e))}}return{width:t,height:o,data:n,clips:a,segments:SCENE_CROWD_SEGMENTS,actions:SCENE_CROWD_ACTIONS.length}}function sceneCrowdVertex(e,t,o,n,a,r,i,c,s,l){const f=e.v.length/12;return e.v.push(t,o,n,a,r,i,c,255,s,l,0,0),f}function sceneCrowdBox(e,t,o,n,a,r,i){const c=o[0]/2,s=o[1]/2,l=o[2]/2,f=t[0],u=t[1],d=t[2],x=n[0],h=n.length>1?n[1]:n[0];if(!i){const _=e.v.length/12;for(let w=0;w<8;w++){const y=w&1?1:-1,E=w&2?1:-1,m=w&4?1:-1,C=Math.hypot(y*s*l,E*c*l,m*c*s)||1;sceneCrowdVertex(e,f+y*c,u+E*s,d+m*l,y*s*l/C,E*c*l/C,m*c*s/C,E>0?x:h,a,r)}const p=[[0,4,6,2],[1,3,7,5],[0,1,5,4],[2,6,7,3],[0,2,3,1],[4,5,7,6]];for(const w of p)e.i.push(_+w[0],_+w[1],_+w[2],_+w[0],_+w[2],_+w[3]);return}const v=[[[1,0,0],[1,-1,-1],[1,1,-1],[1,1,1],[1,-1,1]],[[-1,0,0],[-1,-1,1],[-1,1,1],[-1,1,-1],[-1,-1,-1]],[[0,1,0],[-1,1,-1],[-1,1,1],[1,1,1],[1,1,-1]],[[0,-1,0],[-1,-1,1],[-1,-1,-1],[1,-1,-1],[1,-1,1]],[[0,0,1],[-1,-1,1],[1,-1,1],[1,1,1],[-1,1,1]],[[0,0,-1],[1,-1,-1],[-1,-1,-1],[-1,1,-1],[1,1,-1]]];for(const _ of v){const p=_[0],w=e.v.length/12;for(let y=1;y<=4;y++){const E=_[y];sceneCrowdVertex(e,f+E[0]*c,u+E[1]*s,d+E[2]*l,p[0],p[1],p[2],E[1]>0?x:h,a,r)}e.i.push(w,w+1,w+2,w,w+2,w+3)}}function sceneCrowdQuad(e,t,o,n,a,r,i,c,s){const l=i[0],f=i.length>1?i[1]:i[0],u=e.v.length/12;sceneCrowdVertex(e,t,o,r,0,0,1,f,c,s),sceneCrowdVertex(e,n,o,r,0,0,1,f,c,s),sceneCrowdVertex(e,n,a,r,0,0,1,l,c,s),sceneCrowdVertex(e,t,a,r,0,0,1,l,c,s),e.i.push(u,u+1,u+2,u,u+2,u+3)}function sceneCrowdProps(e,t){sceneCrowdBox(e,[-.22,.85,.03],[.06,.1,.03],[6],4,1,t);const o=e.v.length/12;sceneCrowdBox(e,[0,.84,.04],[.42,.13,.02],[6],3,2,t);for(let n=o;n<e.v.length/12;n++)e.v[n*12]>0&&(e.v[n*12+6]=4);sceneCrowdBox(e,[-.22,.68,.03],[.03,.34,.22],[6],3,3,t),sceneCrowdBox(e,[-.22,1.2,.03],[.025,.8,.025],[6],5,4,t),sceneCrowdBox(e,[-.41,1.45,.03],[.36,.24,.015],[6],3,4,t)}function sceneCrowdFinish(e,t){const o=t/1.75,n=e.v.length/12,a=new ArrayBuffer(n*SCENE_CROWD_VERTEX_BYTES),r=new Float32Array(a),i=new Uint8Array(a);for(let c=0;c<n;c++){const s=c*12,l=c*9,f=c*SCENE_CROWD_VERTEX_BYTES;r[l]=e.v[s]*o,r[l+1]=e.v[s+1]*o,r[l+2]=e.v[s+2]*o,r[l+3]=e.v[s+3],r[l+4]=e.v[s+4],r[l+5]=e.v[s+5],i[f+24]=e.v[s+6],i[f+25]=0,i[f+26]=0,i[f+27]=0,i[f+28]=255,i[f+29]=0,i[f+30]=0,i[f+31]=0,i[f+32]=e.v[s+8],i[f+33]=e.v[s+9],i[f+34]=0,i[f+35]=0}return{vertices:new Uint8Array(a),indices:new Uint16Array(e.i),vertexCount:n,indexCount:e.i.length}}function sceneCrowdMeshes(e){const t={v:[],i:[]};sceneCrowdBox(t,[0,1.2,0],[.38,.5,.22],[1],0,0,!0),sceneCrowdBox(t,[0,1.6,.01],[.2,.24,.22],[2],1,0,!0),sceneCrowdBox(t,[0,1.74,.03],[.23,.06,.27],[2],3,5,!0);for(const a of[1,-1]){const r=a>0?3:5,i=a>0?4:6,c=a>0?7:9,s=a>0?8:10;sceneCrowdBox(t,[.22*a,1.28,0],[.1,.3,.11],[r],0,0,!0),sceneCrowdBox(t,[.22*a,.99,0],[.085,.28,.095],[i],1,0,!0),sceneCrowdBox(t,[.1*a,.71,0],[.15,.44,.16],[c],2,0,!0),sceneCrowdBox(t,[.1*a,.27,0],[.13,.46,.14],[s],2,0,!0),sceneCrowdBox(t,[.1*a,.03,.05],[.12,.06,.24],[s],2,0,!0)}sceneCrowdProps(t,!0);const o={v:[],i:[]};sceneCrowdBox(o,[0,1.2,0],[.38,.5,.22],[1],0,0,!1),sceneCrowdBox(o,[0,1.62,.01],[.21,.27,.23],[2],1,0,!1);for(const a of[1,-1])sceneCrowdBox(o,[.22*a,1.14,0],[.1,.58,.1],a>0?[3,4]:[5,6],0,0,!1);sceneCrowdBox(o,[0,.49,0],[.32,.9,.16],[7,8],2,0,!1),sceneCrowdProps(o,!1);const n={v:[],i:[]};return sceneCrowdQuad(n,-.2,.05,.2,1.45,0,[1,8],0,0),sceneCrowdQuad(n,-.11,1.47,.11,1.75,.01,[2],1,0),sceneCrowdQuad(n,.17,.86,.27,1.43,.005,[3,4],0,0),sceneCrowdQuad(n,-.27,.86,-.17,1.43,.005,[5,6],0,0),sceneCrowdQuad(n,-.6,1.33,-.2,1.6,.02,[6],3,4),sceneCrowdQuad(n,-.3,.76,-.12,.94,.02,[6],4,1),[sceneCrowdFinish(t,e),sceneCrowdFinish(o,e),sceneCrowdFinish(n,e)]}const SCENE_CROWD_SECTION_TEXELS=12,SCENE_CROWD_UNIFORM_VEC4S=32,SCENE_CROWD_MAX_WAVES=4,SCENE_CROWD_CLOCK_SLACK=.25,SCENE_CROWD_FLAG_SYNC=1,SCENE_CROWD_FLAG_NO_WAVE=2,SCENE_CROWD_FLAG_PROPS=4,SCENE_CROWD_FLAG_STANDING=8;function sceneCrowdLinear(e){let t=typeof e=="string"?e.trim().replace(/^#/,""):"";(t.length===3||t.length===4)&&(t=t.slice(0,3).split("").map(function(n){return n+n}).join(""));const o=/^[0-9a-fA-F]{6}/.test(t)?parseInt(t.slice(0,6),16):8421504;return[o>>16&255,o>>8&255,o&255].map(function(n){const a=n/255;return a<=.04045?a/12.92:Math.pow((a+.055)/1.055,2.4)})}const sceneCrowdStores=new WeakMap,sceneCrowdPending=new Map,sceneCrowdClock=new WeakMap;function sceneCrowdMountFor(e){if(e&&typeof e=="object"&&typeof e.getAttribute=="function")return e;const t=typeof e=="string"?e.trim():"";if(!t||typeof document>"u")return null;const o=typeof document.getElementById=="function"?document.getElementById(t):null;if(o)return o;const n=typeof window<"u"&&window.__gosx&&window.__gosx.engines,a=n&&typeof n.get=="function"?n.get(t):null;return a&&a.mount||null}function sceneCrowdStoreMap(e){let t=sceneCrowdStores.get(e);return t||(t=new Map,sceneCrowdStores.set(e,t)),t}function sceneCrowdInitialSections(e){const t=e.sections||[],o=new Float32Array(t.length*SCENE_CROWD_SECTION_TEXELS*4);for(let n=0;n<t.length;n++){const a=t[n],r=n*SCENE_CROWD_SECTION_TEXELS*4,i=a.action|0;o.set([i,i,-1e6,.4],r);const c=Number(a.excitement)||0,s=SCENE_CROWD_FLAG_PROPS|(a.noWave?SCENE_CROWD_FLAG_NO_WAVE:0)|(a.standing?SCENE_CROWD_FLAG_STANDING:0);o.set([c,c,1,s],r+4),o.set([0,.12,-1e6,.001],r+8),sceneCrowdWritePalette(o,r,a)}return o}function sceneCrowdWritePalette(e,t,o){const n=Array.isArray(o.colors)?o.colors:[],a=Array.isArray(o.accents)?o.accents:[];for(let r=0;r<4;r++)e.set(sceneCrowdLinear(n[Math.min(r,Math.max(0,n.length-1))]||"#888888").concat(1),t+(3+r)*4),e.set(sceneCrowdLinear(a[Math.min(r,Math.max(0,a.length-1))]||"#cccccc").concat(1),t+(7+r)*4)}function sceneCrowdStore(e,t){const o=sceneCrowdStoreMap(e);let n=o.get(t.id);const a=(t.sections||[]).map(function(c){return c.id}).join(`
`);if(!n||n.ids!==a)n={ir:t,ids:a,sections:sceneCrowdInitialSections(t),waves:new Float32Array(SCENE_CROWD_MAX_WAVES*8),version:1,lastNow:0},o.set(t.id,n);else if(n.ir!==t){for(let c=0;c<t.sections.length;c++)sceneCrowdWritePalette(n.sections,c*SCENE_CROWD_SECTION_TEXELS*4,t.sections[c]);n.ir=t,n.version++}const r=e&&e.id,i=r&&sceneCrowdPending.get(r);if(i){sceneCrowdPending.delete(r);for(const c of i)sceneCrowdApplyDecoded(e,c)}return n}function sceneCrowdDecode(e){const t=e instanceof ArrayBuffer?new Uint8Array(e):e;if(!(t instanceof Uint8Array))throw new TypeError("crowd frame must be an ArrayBuffer or Uint8Array");const o=new DataView(t.buffer,t.byteOffset,t.byteLength);let n=0;function a(h){if(n+h>t.length)throw new Error("crowd frame is truncated")}if(a(4),t[0]!==71||t[1]!==83||t[2]!==67||t[3]!==49)throw new Error("not a GSC1 crowd frame");n=4,a(2);const r=o.getUint16(n,!0);n+=2,a(r);const i=new TextDecoder().decode(t.subarray(n,n+r));n+=r,a(10);const c=o.getFloat64(n,!0),s=Number.isFinite(c)?c:0;n+=8;const l=o.getUint16(n,!0);n+=2;const f=[];for(let h=0;h<l;h++){a(42);const v={section:o.getUint16(n,!0),from:t[n+2],to:t[n+3],blendStart:s+o.getFloat32(n+4,!0),blend:o.getFloat32(n+8,!0),excitementFrom:o.getFloat32(n+12,!0),excitementTo:o.getFloat32(n+16,!0),rate:o.getFloat32(n+20,!0),epoch:s+o.getFloat32(n+24,!0),jitter:o.getFloat32(n+28,!0),excitementStart:s+o.getFloat32(n+32,!0),excitementRamp:o.getFloat32(n+36,!0),flags:t[n+40]};if(n+=42,v.from>8||v.to>8)throw new Error("crowd frame names an unknown action");f.push(v)}a(2);const u=t[n],d=t[n+1]===1;if(n+=2,u>SCENE_CROWD_MAX_WAVES)throw new Error("crowd frame has more than four waves");const x=[];for(let h=0;h<u;h++)a(20),x.push({start:s+o.getFloat32(n,!0),origin:o.getFloat32(n+4,!0),speed:o.getFloat32(n+8,!0),width:o.getFloat32(n+12,!0),laps:o.getFloat32(n+16,!0)}),n+=20;if(n!==t.length)throw new Error("crowd frame has trailing bytes");return{crowd:i,now:s,sections:f,waves:x,replaceWaves:d}}function sceneCrowdOffset(e,t){const o=sceneCrowdClock.get(e)||{scene:0,offset:null},n=o.scene-t;return(o.offset===null||Math.abs(n-o.offset)>SCENE_CROWD_CLOCK_SLACK)&&(o.offset=n),sceneCrowdClock.set(e,o),o.offset}function sceneCrowdApplyDecoded(e,t){const o=sceneCrowdStoreMap(e).get(t.crowd);if(!o)return!1;const n=sceneCrowdOffset(e,t.now),a=o.ir.sections.length;for(const r of t.sections){if(r.section>=a)continue;const i=r.section*SCENE_CROWD_SECTION_TEXELS*4,c=o.sections,s=c[i+7]&(SCENE_CROWD_FLAG_NO_WAVE|SCENE_CROWD_FLAG_STANDING);c[i]=r.from,c[i+1]=r.to,c[i+2]=r.blendStart+n,c[i+3]=Math.max(.001,r.blend),c[i+4]=r.excitementFrom,c[i+5]=r.excitementTo,c[i+6]=r.rate>0?r.rate:1,c[i+7]=s|r.flags&(SCENE_CROWD_FLAG_SYNC|SCENE_CROWD_FLAG_PROPS),c[i+8]=r.epoch+n,c[i+9]=r.jitter,c[i+10]=r.excitementStart+n,c[i+11]=Math.max(.001,r.excitementRamp)}t.replaceWaves&&o.waves.fill(0);for(const r of t.waves){let i=0;for(let c=0;c<SCENE_CROWD_MAX_WAVES;c++){const s=c*8,l=o.waves[s+3],f=Math.abs(o.waves[s+2]);if(l<=0||f<=0||f*(o.lastNow-o.waves[s])>o.waves[s+4]+l){i=c;break}}o.waves.set([r.start+n,r.origin,r.speed,Math.max(1e-4,r.width),Math.max(1,r.laps),0,0,0],i*8)}return o.version++,!0}function sceneCrowdApply(e,t){const o=t instanceof Uint8Array||t instanceof ArrayBuffer?sceneCrowdDecode(t):t,n=sceneCrowdMountFor(e);if(n&&sceneCrowdStoreMap(n).has(o.crowd))return sceneCrowdApplyDecoded(n,o);const a=n&&n.id||(typeof e=="string"?e.trim():"");if(!a)return!1;const r=sceneCrowdPending.get(a)||[];return r.push(o),sceneCrowdPending.set(a,r.slice(-64)),!1}function sceneCrowdNoteClock(e,t){const o=sceneCrowdClock.get(e)||{scene:0,offset:null};o.scene=t,sceneCrowdClock.set(e,o)}function sceneCrowdModel(e){const t=Number(e.rotation)||0,o=Math.cos(t),n=Math.sin(t);return new Float32Array([o,0,-n,0,0,1,0,0,n,0,o,0,Number(e.x)||0,Number(e.y)||0,Number(e.z)||0,1])}function sceneCrowdUniforms(e,t,o,n,a,r){r.fill(0),r.set(n,0),r[16]=a,r[17]=Number(e.rotation)||0,r[19]=Number(e.material&&e.material.lightGlow)||1.5;const i=e.fan||{},c=Array.isArray(i.skinTones)&&i.skinTones.length?i.skinTones.slice(0,8):["#c68c5f"];r.set(sceneCrowdLinear(i.legs||"#2b2f3a"),20),r[23]=c.length;for(let s=0;s<c.length;s++)r.set(sceneCrowdLinear(c[s]).concat(1),24+s*4);return r.set(o.clips,56),r.set(t.waves,92),r[124]=(e.sections||[]).length,r}function sceneCrowdPlanes(e,t){const o=[0,1,2,3].map(function(a){return[e[a],e[4+a],e[8+a],e[12+a]]}),n=[[0,1],[0,-1],[1,1],[1,-1],[2,1],[2,-1]];for(let a=0;a<6;a++){const r=o[n[a][0]],i=n[a][1],c=o[3][0]+i*r[0],s=o[3][1]+i*r[1],l=o[3][2]+i*r[2],f=o[3][3]+i*r[3],u=Math.hypot(c,s,l)||1;t[a*4]=c/u,t[a*4+1]=s/u,t[a*4+2]=l/u,t[a*4+3]=f/u}return t}function sceneCrowdSelect(e,t,o,n){const a=o.lod||{},r=t.blocks,i=t.spheres,c=e.model,s=r.length/3;(!e.select||e.select.tiers.length!==s)&&(e.select={tiers:new Int8Array(s),dist:new Float32Array(s),order:[],planes:new Float64Array(24)});const l=e.select,f=sceneCrowdPlanes(n.viewProj,l.planes),u=(Number(o.fan&&o.fan.height)||1.75)*1.06,d=Math.abs(Number(n.proj&&n.proj[5])||1.5),x=Math.max(1,Number(n.height)||1),h=Number(a.fullPixels)||64,v=Number(a.lowPixels)||20,_=Number(a.maxFull)||8e3,p=n.camera||{},w=Number(p.x)||0,y=Number(p.y)||0,E=Number(p.z)||0,m=[];for(let g=0;g<s;g++){const A=i[g*4],P=i[g*4+1],M=i[g*4+2],T=i[g*4+3],R=c[0]*A+c[8]*M+c[12],U=P+c[13],N=c[2]*A+c[10]*M+c[14];let z=!0;for(let k=0;k<6&&z;k++)z=f[k*4]*R+f[k*4+1]*U+f[k*4+2]*N+f[k*4+3]>-T;if(!z){l.tiers[g]=-1;continue}const D=Math.max(.5,Math.hypot(R-w,U-y,N-E)-T),B=u*d*x/(2*D);l.dist[g]=D,l.tiers[g]=B>=h?0:B>=v?1:2,l.tiers[g]===0&&m.push(g)}let C=0;for(const g of m)C+=r[g*3+1];if(C>_){m.sort(function(A,P){return l.dist[A]-l.dist[P]});let g=0;for(const A of m)g+r[A*3+1]>_?l.tiers[A]=1:g+=r[A*3+1]}const b=[[],[],[]],S=[0,0,0];for(let g=0;g<s;g++){const A=l.tiers[g];if(A<0)continue;const P=r[g*3],M=r[g*3+1],T=b[A];S[A]+=M,T.length&&T[T.length-2]+T[T.length-1]===P?T[T.length-1]+=M:T.push(P,M)}return{ranges:b,fans:S}}function sceneCrowdLevels(e,t){const o=sceneCrowdMountFor(e),n=o&&sceneCrowdStoreMap(o).get(t);if(!n)return null;const a=[.08,.3,1,.65,.75,.9,.7,.45,.85],r=n.lastNow,i=[];let c=0,s=0;for(let l=0;l<n.ir.sections.length;l++){const f=l*SCENE_CROWD_SECTION_TEXELS*4,u=n.sections,d=Math.min(1,Math.max(0,(r-u[f+10])/Math.max(.001,u[f+11]))),x=u[f+4]+(u[f+5]-u[f+4])*d,h=n.fans?n.fans[l]:0;i.push({id:n.ir.sections[l].id,action:SCENE_CROWD_ACTIONS[u[f+1]|0][0],excitement:x,fans:h}),c+=h,s+=h*x*a[u[f+1]|0]}return{now:r,loudness:c?s/c:0,sections:i}}const SCENE_CROWD_GLSL_VERTEX=`#version 300 es
precision highp float;
precision highp int;
layout(location = 0) in vec3 a_position;
layout(location = 1) in vec3 a_normal;
layout(location = 2) in uvec4 a_joints;
layout(location = 3) in vec4 a_weights;
layout(location = 4) in uvec4 a_part;
layout(location = 5) in vec4 a_seat;
layout(location = 6) in float a_wave;
layout(location = 7) in uvec2 a_ids;
layout(location = 8) in uvec4 a_look;
layout(location = 9) in uvec4 a_extra;
uniform mat4 u_viewMatrix;
uniform mat4 u_projectionMatrix;
uniform vec3 u_cameraPosition;
uniform vec4 u_crowd[32];
uniform highp sampler2D u_crowdPalette;
uniform highp sampler2D u_crowdSections;
out vec3 v_worldPosition;
out vec3 v_normal;
out vec2 v_uv;
out vec3 v_tangent;
out vec3 v_bitangent;
flat out vec4 v_instanceColor;
flat out vec4 v_crowdGlow;
vec4 crowdSection(int s, int t) { return texelFetch(u_crowdSections, ivec2(t, s), 0); }
mat4 crowdJoint(int j, int row) {
  int x = j * 4;
  return mat4(texelFetch(u_crowdPalette, ivec2(x, row), 0), texelFetch(u_crowdPalette, ivec2(x + 1, row), 0),
    texelFetch(u_crowdPalette, ivec2(x + 2, row), 0), texelFetch(u_crowdPalette, ivec2(x + 3, row), 0));
}
mat4 crowdPose(int a, float ph, int j) {
  vec4 clip = u_crowd[14 + a];
  float f = (a == 8 ? clamp(ph, 0.0, 1.0) : fract(ph)) * clip.y;
#if CROWD_TIER > 0
  return crowdJoint(j, int(clip.x + floor(f + 0.5)));
#else
  float f0 = min(floor(f), clip.y - 1.0);
  mat4 m0 = crowdJoint(j, int(clip.x + f0));
  mat4 m1 = crowdJoint(j, int(clip.x + f0) + 1);
  return m0 + (m1 - m0) * (f - f0);
#endif
}
// crowdJoin is how far a fan has joined its section's action (0 idles, 1
// joined). A fan joins once the excitement ramp passes its eagerness, over
// 0.4 seconds, and leaves the same way.
float crowdJoin(vec4 t1, vec4 t2, float eager, float now) {
  float tau = (eager - 64.0) / 192.0;
  bool was = t1.x > tau, is = t1.y > tau;
  if (was == is) return is ? 1.0 : 0.0;
  float crossing = t2.z + (tau - t1.x) / (t1.y - t1.x) * max(t2.w, 0.001);
  float j = clamp((now - crossing) / 0.4, 0.0, 1.0);
  return is ? j : 1.0 - j;
}
float crowdPhase(int a, bool synced, vec4 t1, vec4 t2, float now, float seed, float variant) {
  float cycle = max(u_crowd[14 + a].z, 0.05);
  if (synced) return (now - t2.x) * t1.z / cycle + t2.y * (seed - 0.5);
  return now * t1.z * (0.92 + 0.16 * variant) / cycle + seed;
}
void main() {
  int s = int(a_ids.x);
  vec4 t0 = crowdSection(s, 0), t1 = crowdSection(s, 1), t2 = crowdSection(s, 2);
  int flags = int(t1.w + 0.5);
  float now = u_crowd[4].x;
  float eager = float(a_extra.x);
  float variant = float(a_look.w) / 255.0;
  float seed = float(a_ids.y) / 65536.0;
  int rest = (flags & 8) != 0 ? 1 : 0;
  int fanA = int(t0.x + 0.5), fanB = int(t0.y + 0.5);
  float join = crowdJoin(t1, t2, eager, now);
  join = join * join * (3.0 - 2.0 * join);
  float k = clamp((now - t0.z - eager / 255.0 * 0.6) / max(t0.w, 0.001), 0.0, 1.0);
  k = k * k * (3.0 - 2.0 * k);
  bool synced = (flags & 1) != 0;
  float phA = crowdPhase(fanA, synced, t1, t2, now, seed, variant);
  float phB = crowdPhase(fanB, synced, t1, t2, now, seed, variant);
  float phR = crowdPhase(rest, false, t1, t2, now, seed, variant);
  float waveU = -1.0;
  if ((flags & 2) == 0 && a_extra.x < 246u) {
    for (int w = 0; w < 4; w++) {
      vec4 wa = u_crowd[23 + w * 2], wb = u_crowd[24 + w * 2];
      if (wa.w <= 0.0 || wa.z == 0.0 || now < wa.x || waveU >= 0.0) continue;
      float x = abs(wa.z) * (now - wa.x) - fract(sign(wa.z) * (a_wave - wa.y));
      float u = (x - clamp(floor(x + 0.5), 0.0, wb.x - 1.0)) / wa.w + 0.5;
      if (u > 0.0 && u < 1.0) waveU = u;
    }
  }
  float waveW = waveU < 0.0 ? 0.0 : smoothstep(0.0, 0.2, waveU) * (1.0 - smoothstep(0.8, 1.0, waveU));
  mat4 skin = mat4(0.0);
  for (int i = 0; i < 4; i++) {
    float wt = a_weights[i];
    if (wt <= 0.0) continue;
    int j = int(a_joints[i]);
    mat4 m;
    if (join <= 0.0) m = crowdPose(rest, phR, j);
    else {
      m = crowdPose(fanB, phB, j);
      if (k < 1.0 && fanA != fanB) { mat4 ma = crowdPose(fanA, phA, j); m = ma + (m - ma) * k; }
      if (join < 1.0) { mat4 mr = crowdPose(rest, phR, j); m = mr + (m - mr) * join; }
    }
    if (waveW > 0.0) m = m + (crowdPose(8, waveU, j) - m) * waveW;
    skin += m * wt;
  }
  vec3 local = (skin * vec4(a_position, 1.0)).xyz;
  vec3 n = mat3(skin) * a_normal;
  float scale = 0.94 + 0.12 * float(a_look.w & 15u) / 15.0;
  mat4 model = mat4(u_crowd[0], u_crowd[1], u_crowd[2], u_crowd[3]);
  float yaw = a_seat.w;
#if CROWD_TIER == 2
  vec3 seatWorld = (model * vec4(a_seat.xyz, 1.0)).xyz;
  vec3 toCamera = u_cameraPosition - seatWorld;
  yaw = atan(toCamera.x, toCamera.z) - u_crowd[4].y;
  n = normalize(n + vec3(0.0, 0.6, 0.0));
#endif
  float cy = cos(yaw), sy = sin(yaw);
  vec3 p = vec3(cy * local.x + sy * local.z, local.y, -sy * local.x + cy * local.z) * scale + a_seat.xyz;
  vec3 nr = vec3(cy * n.x + sy * n.z, n.y, -sy * n.x + cy * n.z);
  uint kind = a_part.y;
  bool hidden = kind == 5u ? (a_look.w & 0x90u) != 0x90u : (kind > 0u && (kind != a_look.z || (flags & 4) == 0));
  vec4 world = model * vec4(p, 1.0);
  v_worldPosition = world.xyz;
  v_normal = normalize(mat3(model) * nr);
  v_uv = vec2(0.0);
  v_tangent = normalize(abs(v_normal.y) < 0.9 ? cross(vec3(0.0, 1.0, 0.0), v_normal) : cross(vec3(1.0, 0.0, 0.0), v_normal));
  v_bitangent = cross(v_normal, v_tangent);
  uint slot = a_part.x;
  vec3 color = vec3(0.25, 0.2, 0.15);
  if (slot == 0u) color = crowdSection(s, 3 + int(a_look.x)).rgb;
  else if (slot == 1u) color = u_crowd[6 + int((a_look.w >> 4) & 7u) % max(int(u_crowd[5].w), 1)].rgb;
  else if (slot == 2u) color = u_crowd[5].rgb;
  else if (slot == 3u || slot == 4u) color = crowdSection(s, 7 + int(a_look.y)).rgb;
  v_instanceColor = vec4(color, 1.0);
  float flicker = 0.8 + 0.2 * sin(now * 7.0 + seed * 40.0);
  v_crowdGlow = slot == 4u ? vec4(color * u_crowd[4].w * flicker, 1.0) : vec4(0.0);
  gl_Position = hidden ? vec4(2.0, 2.0, 2.0, 1.0) : u_projectionMatrix * u_viewMatrix * world;
}`;function sceneCrowdFragmentGLSL(e){const t="void gosxApplyCustomFragment(inout vec3 color, inout float opacity, vec3 normal, vec3 worldPosition, vec2 uv) {}";if(e.indexOf(t)<0)throw new Error("crowd: PBR fragment hook missing");return e.replace(t,`flat in vec4 v_crowdGlow;
void gosxApplyCustomFragment(inout vec3 color, inout float opacity, vec3 normal, vec3 worldPosition, vec2 uv) { color += v_crowdGlow.rgb * (u_outputLinear == 1 ? 2.0 : 0.8); }`)}function sceneCrowdGLShader(e,t,o){const n=e.createShader(t);if(e.shaderSource(n,o),e.compileShader(n),!e.getShaderParameter(n,e.COMPILE_STATUS)){const a=e.getShaderInfoLog(n);throw e.deleteShader(n),new Error("crowd shader: "+a)}return n}function sceneCrowdGLProgram(e,t,o,n){if(e.programs[n])return e.programs[n];const a=typeof o.fragment=="function"?o.fragment():o.fragment,r=sceneCrowdGLShader(t,t.VERTEX_SHADER,SCENE_CROWD_GLSL_VERTEX.replace("precision highp int;",`precision highp int;
#define CROWD_TIER `+n)),i=sceneCrowdGLShader(t,t.FRAGMENT_SHADER,sceneCrowdFragmentGLSL(String(a||""))),c=t.createProgram();if(t.attachShader(c,r),t.attachShader(c,i),t.linkProgram(c),t.deleteShader(r),t.deleteShader(i),!t.getProgramParameter(c,t.LINK_STATUS)){const l=t.getProgramInfoLog(c);throw t.deleteProgram(c),new Error("crowd program: "+l)}const s=typeof o.uniforms=="function"?o.uniforms(t,c):{};return s.crowd=t.getUniformLocation(c,"u_crowd[0]")||t.getUniformLocation(c,"u_crowd"),s.crowdPalette=t.getUniformLocation(c,"u_crowdPalette"),s.crowdSections=t.getUniformLocation(c,"u_crowdSections"),e.programs[n]={program:c,uniforms:s},e.programs[n]}function sceneCrowdGLTexture(e,t,o,n){const a=e.createTexture();return e.bindTexture(e.TEXTURE_2D,a),e.texImage2D(e.TEXTURE_2D,0,e.RGBA32F,t,o,0,e.RGBA,e.FLOAT,n),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MIN_FILTER,e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MAG_FILTER,e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_S,e.CLAMP_TO_EDGE),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_T,e.CLAMP_TO_EDGE),a}function sceneCrowdGLInstanceAttributes(e,t){e.vertexAttribPointer(5,4,e.FLOAT,!1,32,t),e.vertexAttribPointer(6,1,e.FLOAT,!1,32,t+16),e.vertexAttribIPointer(7,2,e.UNSIGNED_SHORT,32,t+20),e.vertexAttribIPointer(8,4,e.UNSIGNED_BYTE,32,t+24),e.vertexAttribIPointer(9,4,e.UNSIGNED_BYTE,32,t+28)}function sceneCrowdGLStatic(e,t,o){if(e.meshes&&e.fanHeight===o)return;if(e.meshes)for(const a of e.meshes)t.deleteBuffer(a.vbo),t.deleteBuffer(a.ibo),t.deleteVertexArray(a.vao);e.palette&&t.deleteTexture(e.palette),e.fanHeight=o,e.bake=sceneCrowdBake(o),e.palette=sceneCrowdGLTexture(t,e.bake.width,e.bake.height,e.bake.data);const n=t.getParameter(t.VERTEX_ARRAY_BINDING);e.meshes=sceneCrowdMeshes(o).map(function(a){const r=t.createVertexArray();t.bindVertexArray(r);const i=t.createBuffer();t.bindBuffer(t.ARRAY_BUFFER,i),t.bufferData(t.ARRAY_BUFFER,a.vertices,t.STATIC_DRAW);for(let s=0;s<5;s++)t.enableVertexAttribArray(s);t.vertexAttribPointer(0,3,t.FLOAT,!1,36,0),t.vertexAttribPointer(1,3,t.FLOAT,!1,36,12),t.vertexAttribIPointer(2,4,t.UNSIGNED_BYTE,36,24),t.vertexAttribPointer(3,4,t.UNSIGNED_BYTE,!0,36,28),t.vertexAttribIPointer(4,4,t.UNSIGNED_BYTE,36,32);const c=t.createBuffer();return t.bindBuffer(t.ELEMENT_ARRAY_BUFFER,c),t.bufferData(t.ELEMENT_ARRAY_BUFFER,a.indices,t.STATIC_DRAW),{vao:r,vbo:i,ibo:c,indexCount:a.indexCount}}),t.bindVertexArray(n)}function sceneCrowdGLCrowd(e,t,o,n){let a=e.crowds.get(o.id);if(!a||a.ir!==o){a&&sceneCrowdGLDisposeCrowd(t,a);const r=sceneCrowdPack(o);if(r.error)throw new Error(r.error);a={ir:o,packed:r,model:sceneCrowdModel(o),version:-1,uniforms:new Float32Array(SCENE_CROWD_UNIFORM_VEC4S*4)},a.instances=t.createBuffer(),t.bindBuffer(t.ARRAY_BUFFER,a.instances),t.bufferData(t.ARRAY_BUFFER,r.records,t.STATIC_DRAW),a.sections=sceneCrowdGLTexture(t,SCENE_CROWD_SECTION_TEXELS,o.sections.length,n.sections),a.version=n.version,n.fans=r.sectionFans,e.crowds.set(o.id,a)}return a.version!==n.version&&(t.bindTexture(t.TEXTURE_2D,a.sections),t.texSubImage2D(t.TEXTURE_2D,0,0,0,SCENE_CROWD_SECTION_TEXELS,o.sections.length,t.RGBA,t.FLOAT,n.sections),a.version=n.version),a}function sceneCrowdGLDisposeCrowd(e,t){t.instances&&e.deleteBuffer(t.instances),t.sections&&e.deleteTexture(t.sections),t.instances=t.sections=null}function sceneCrowdGLDispose(e){const t=sceneCrowdGLResources.get(e);if(t){sceneCrowdGLResources.delete(e);for(const o of t.crowds.values())sceneCrowdGLDisposeCrowd(e,o);for(const o of t.programs)o&&e.deleteProgram(o.program);if(t.meshes)for(const o of t.meshes)e.deleteBuffer(o.vbo),e.deleteBuffer(o.ibo),e.deleteVertexArray(o.vao);t.palette&&e.deleteTexture(t.palette)}}const sceneCrowdGLResources=new WeakMap;function sceneCrowdGLDraw(e,t){const o=t.environment&&Array.isArray(t.environment.crowds)?t.environment.crowds:null,n={state:"none",fans:0,full:0,low:0,impostor:0,draws:0,cpuMs:0,error:""},a=typeof performance<"u"?performance.now():0;if(!o||!o.length)return sceneCrowdGLResources.has(e)&&sceneCrowdGLDispose(e),sceneCrowdPublish(t.mount,n),n.state;let r=sceneCrowdGLResources.get(e);if(!r){r={crowds:new Map,programs:[null,null,null],ext:e.getExtension("WEBGL_draw_instanced_base_vertex_base_instance")};const s=e.getParameter(e.MAX_COMBINED_TEXTURE_IMAGE_UNITS),l=e.getParameter(e.MAX_TEXTURE_IMAGE_UNITS);r.units=s-4>=l?[s-4,s-3]:[l-2,l-1],sceneCrowdGLResources.set(e,r)}const i=e.getParameter(e.VERTEX_ARRAY_BINDING),c=e.isEnabled(e.CULL_FACE);try{const s=Number(t.time)||0;sceneCrowdNoteClock(t.mount,s);const l=new Set;for(const f of o){if(!f||!f.id)continue;l.add(f.id);const u=sceneCrowdStore(t.mount,f);u.lastNow=s,sceneCrowdGLStatic(r,e,Number(f.fan&&f.fan.height)||1.75);const d=sceneCrowdGLCrowd(r,e,f,u),x=sceneCrowdSelect(d,d.packed,f,t);n.fans+=d.packed.count,n.full+=x.fans[0],n.low+=x.fans[1],n.impostor+=x.fans[2],sceneCrowdUniforms(f,u,r.bake,d.model,s,d.uniforms);for(let h=0;h<3;h++){const v=x.ranges[h];if(!v.length)continue;const _=sceneCrowdGLProgram(r,e,t,h),p=_.uniforms,w=r.meshes[h];e.useProgram(_.program),typeof t.prepare=="function"&&t.prepare(p,{color:"#ffffff",roughness:Number(f.material&&f.material.roughness)||.85,metalness:0}),p.receiveShadow&&e.uniform1i(p.receiveShadow,f.receiveShadow===!1?0:1),e.uniform4fv(p.crowd,d.uniforms),e.activeTexture(e.TEXTURE0+r.units[0]),e.bindTexture(e.TEXTURE_2D,r.palette),e.uniform1i(p.crowdPalette,r.units[0]),e.activeTexture(e.TEXTURE0+r.units[1]),e.bindTexture(e.TEXTURE_2D,d.sections),e.uniform1i(p.crowdSections,r.units[1]),e.disable(e.BLEND),e.enable(e.DEPTH_TEST),e.depthMask(!0),e.depthFunc(e.LEQUAL),e.enable(e.CULL_FACE),e.cullFace(e.BACK),e.bindVertexArray(w.vao),e.bindBuffer(e.ARRAY_BUFFER,d.instances);for(let y=5;y<10;y++)e.enableVertexAttribArray(y),e.vertexAttribDivisor(y,1);r.ext&&sceneCrowdGLInstanceAttributes(e,0);for(let y=0;y<v.length;y+=2)r.ext?r.ext.drawElementsInstancedBaseVertexBaseInstanceWEBGL(e.TRIANGLES,w.indexCount,e.UNSIGNED_SHORT,0,v[y+1],0,v[y]):(sceneCrowdGLInstanceAttributes(e,v[y]*32),e.drawElementsInstanced(e.TRIANGLES,w.indexCount,e.UNSIGNED_SHORT,0,v[y+1])),n.draws++}}for(const f of Array.from(r.crowds.keys()))l.has(f)||(sceneCrowdGLDisposeCrowd(e,r.crowds.get(f)),r.crowds.delete(f));n.state="ready"}catch(s){n.state="failed",n.error=String(s&&s.message||s)}finally{e.bindVertexArray(i),c||e.disable(e.CULL_FACE),e.activeTexture(e.TEXTURE0),t.textureCache&&(t.textureCache._sceneTextureEpoch=(t.textureCache._sceneTextureEpoch||0)+1),t.program&&e.useProgram(t.program),n.cpuMs=typeof performance<"u"?performance.now()-a:0,sceneCrowdPublish(t.mount,n)}return n.state}function sceneCrowdPublish(e,t){const o=typeof window<"u"&&window.__gosx_scene3d_crowd_api;if(o&&(o.stats=t),!e||typeof e.setAttribute!="function")return;const n=t.full+"/"+t.low+"/"+t.impostor,a=t.state+"|"+t.fans+"|"+n+"|"+t.draws+"|"+t.error;e.__gosxCrowdPublished!==a&&(e.__gosxCrowdPublished=a,e.setAttribute("data-gosx-scene3d-crowd",t.state),e.setAttribute("data-gosx-scene3d-crowd-fans",String(t.fans)),e.setAttribute("data-gosx-scene3d-crowd-tiers",n),e.setAttribute("data-gosx-scene3d-crowd-draws",String(t.draws)),t.error?e.setAttribute("data-gosx-scene3d-crowd-error",t.error):typeof e.removeAttribute=="function"&&e.removeAttribute("data-gosx-scene3d-crowd-error"))}function sceneCrowdVertexWGSL(e){return e+`
struct CrowdBlock { v: array<vec4f, 32> };
@group(0) @binding(0) var<uniform> frame: FrameUniforms;
@group(2) @binding(0) var<uniform> crowd: CrowdBlock;
@group(2) @binding(1) var crowdPalette: texture_2d<f32>;
@group(2) @binding(2) var crowdSections: texture_2d<f32>;
override CROWD_TIER: u32 = 0u;
struct CrowdIn {
  @location(0) position: vec3f, @location(1) normal: vec3f, @location(2) joints: vec4<u32>, @location(3) weights: vec4f,
  @location(4) part: vec4<u32>, @location(5) seat: vec4f, @location(6) wave: f32, @location(7) ids: vec2<u32>,
  @location(8) look: vec4<u32>, @location(9) extra: vec4<u32>,
};
struct VertexOutput {
  @builtin(position) clipPos: vec4f,
  @location(0) worldPos: vec3f,
  @location(1) normal: vec3f,
  @location(2) uv: vec2f,
  @location(3) tangent: vec3f,
  @location(4) bitangent: vec3f,
  @location(5) @interpolate(flat) instanceColor: vec4f,
  @location(6) @interpolate(flat) crowdGlow: vec4f,
};
fn crowdSection(s: i32, t: i32) -> vec4f { return textureLoad(crowdSections, vec2i(t, s), 0); }
fn crowdJoint(j: i32, row: i32) -> mat4x4f {
  let x = j * 4;
  return mat4x4f(textureLoad(crowdPalette, vec2i(x, row), 0), textureLoad(crowdPalette, vec2i(x + 1, row), 0),
    textureLoad(crowdPalette, vec2i(x + 2, row), 0), textureLoad(crowdPalette, vec2i(x + 3, row), 0));
}
fn crowdPose(a: i32, ph: f32, j: i32) -> mat4x4f {
  let clip = crowd.v[14 + a];
  var f = fract(ph) * clip.y;
  if (a == 8) { f = clamp(ph, 0.0, 1.0) * clip.y; }
  if (CROWD_TIER > 0u) { return crowdJoint(j, i32(clip.x + floor(f + 0.5))); }
  let f0 = min(floor(f), clip.y - 1.0);
  let m0 = crowdJoint(j, i32(clip.x + f0));
  let m1 = crowdJoint(j, i32(clip.x + f0) + 1);
  return m0 + (m1 - m0) * (f - f0);
}
fn crowdJoin(t1: vec4f, t2: vec4f, eager: f32, now: f32) -> f32 {
  let tau = (eager - 64.0) / 192.0;
  let was = t1.x > tau;
  let is = t1.y > tau;
  if (was == is) { return select(0.0, 1.0, is); }
  let crossing = t2.z + (tau - t1.x) / (t1.y - t1.x) * max(t2.w, 0.001);
  let j = clamp((now - crossing) / 0.4, 0.0, 1.0);
  return select(1.0 - j, j, is);
}
fn crowdPhase(a: i32, synced: bool, t1: vec4f, t2: vec4f, now: f32, seed: f32, variant: f32) -> f32 {
  let cycle = max(crowd.v[14 + a].z, 0.05);
  if (synced) { return (now - t2.x) * t1.z / cycle + t2.y * (seed - 0.5); }
  return now * t1.z * (0.92 + 0.16 * variant) / cycle + seed;
}
@vertex fn vertexMain(in: CrowdIn) -> VertexOutput {
  var out: VertexOutput;
  let s = i32(in.ids.x);
  let t0 = crowdSection(s, 0);
  let t1 = crowdSection(s, 1);
  let t2 = crowdSection(s, 2);
  let flags = i32(t1.w + 0.5);
  let now = crowd.v[4].x;
  let eager = f32(in.extra.x);
  let variant = f32(in.look.w) / 255.0;
  let seed = f32(in.ids.y) / 65536.0;
  let rest = select(0, 1, (flags & 8) != 0);
  let fanA = i32(t0.x + 0.5);
  let fanB = i32(t0.y + 0.5);
  var join = crowdJoin(t1, t2, eager, now);
  join = join * join * (3.0 - 2.0 * join);
  var k = clamp((now - t0.z - eager / 255.0 * 0.6) / max(t0.w, 0.001), 0.0, 1.0);
  k = k * k * (3.0 - 2.0 * k);
  let synced = (flags & 1) != 0;
  let phA = crowdPhase(fanA, synced, t1, t2, now, seed, variant);
  let phB = crowdPhase(fanB, synced, t1, t2, now, seed, variant);
  let phR = crowdPhase(rest, false, t1, t2, now, seed, variant);
  var waveU = -1.0;
  if ((flags & 2) == 0 && in.extra.x < 246u) {
    for (var w = 0; w < 4; w++) {
      let wa = crowd.v[23 + w * 2];
      let wb = crowd.v[24 + w * 2];
      if (wa.w <= 0.0 || wa.z == 0.0 || now < wa.x || waveU >= 0.0) { continue; }
      let x = abs(wa.z) * (now - wa.x) - fract(sign(wa.z) * (in.wave - wa.y));
      let u = (x - clamp(floor(x + 0.5), 0.0, wb.x - 1.0)) / wa.w + 0.5;
      if (u > 0.0 && u < 1.0) { waveU = u; }
    }
  }
  var waveW = 0.0;
  if (waveU >= 0.0) { waveW = smoothstep(0.0, 0.2, waveU) * (1.0 - smoothstep(0.8, 1.0, waveU)); }
  var skin = mat4x4f(vec4f(0.0), vec4f(0.0), vec4f(0.0), vec4f(0.0));
  for (var i = 0; i < 4; i++) {
    let wt = in.weights[i];
    if (wt <= 0.0) { continue; }
    let j = i32(in.joints[i]);
    var m: mat4x4f;
    if (join <= 0.0) { m = crowdPose(rest, phR, j); }
    else {
      m = crowdPose(fanB, phB, j);
      if (k < 1.0 && fanA != fanB) { let ma = crowdPose(fanA, phA, j); m = ma + (m - ma) * k; }
      if (join < 1.0) { let mr = crowdPose(rest, phR, j); m = mr + (m - mr) * join; }
    }
    if (waveW > 0.0) { m = m + (crowdPose(8, waveU, j) - m) * waveW; }
    skin = skin + m * wt;
  }
  let local = (skin * vec4f(in.position, 1.0)).xyz;
  var n = mat3x3f(skin[0].xyz, skin[1].xyz, skin[2].xyz) * in.normal;
  let scale = 0.94 + 0.12 * f32(in.look.w & 15u) / 15.0;
  let model = mat4x4f(crowd.v[0], crowd.v[1], crowd.v[2], crowd.v[3]);
  var yaw = in.seat.w;
  if (CROWD_TIER == 2u) {
    let seatWorld = (model * vec4f(in.seat.xyz, 1.0)).xyz;
    let toCamera = frame.cameraPos - seatWorld;
    yaw = atan2(toCamera.x, toCamera.z) - crowd.v[4].y;
    n = normalize(n + vec3f(0.0, 0.6, 0.0));
  }
  let cy = cos(yaw);
  let sy = sin(yaw);
  let p = vec3f(cy * local.x + sy * local.z, local.y, -sy * local.x + cy * local.z) * scale + in.seat.xyz;
  let nr = vec3f(cy * n.x + sy * n.z, n.y, -sy * n.x + cy * n.z);
  let kind = in.part.y;
  var hidden = kind > 0u && (kind != in.look.z || (flags & 4) == 0);
  if (kind == 5u) { hidden = (in.look.w & 0x90u) != 0x90u; }
  let world = model * vec4f(p, 1.0);
  out.worldPos = world.xyz;
  out.normal = normalize(mat3x3f(model[0].xyz, model[1].xyz, model[2].xyz) * nr);
  out.uv = vec2f(0.0);
  var t = cross(vec3f(1.0, 0.0, 0.0), out.normal);
  if (abs(out.normal.y) < 0.9) { t = cross(vec3f(0.0, 1.0, 0.0), out.normal); }
  out.tangent = normalize(t);
  out.bitangent = cross(out.normal, out.tangent);
  let slot = in.part.x;
  var color = vec3f(0.25, 0.2, 0.15);
  if (slot == 0u) { color = crowdSection(s, 3 + i32(in.look.x)).rgb; }
  else if (slot == 1u) { color = crowd.v[6 + i32((in.look.w >> 4u) & 7u) % max(i32(crowd.v[5].w), 1)].rgb; }
  else if (slot == 2u) { color = crowd.v[5].rgb; }
  else if (slot == 3u || slot == 4u) { color = crowdSection(s, 7 + i32(in.look.y)).rgb; }
  out.instanceColor = vec4f(color, 1.0);
  let flicker = 0.8 + 0.2 * sin(now * 7.0 + seed * 40.0);
  out.crowdGlow = select(vec4f(0.0), vec4f(color * crowd.v[4].w * flicker, 1.0), slot == 4u);
  out.clipPos = frame.projMatrix * frame.viewMatrix * world;
  if (hidden) { out.clipPos = vec4f(2.0, 2.0, 2.0, 1.0); }
  return out;
}`}function sceneCrowdFragmentWGSL(e){const t=/(@location\(5\)\s*@interpolate\(flat\)\s*instanceColor\s*:\s*vec4f\s*,)(\s*\};)/;if(!t.test(e)||e.indexOf("gosxPBRColor(in)")<0)throw new Error("crowd: PBR fragment anchor missing");return e.replace(t,`$1
    @location(6) @interpolate(flat) crowdGlow: vec4f,$2`).split("gosxPBRColor(in)").join("crowdGlowed(in, gosxPBRColor(in))")+`
fn crowdGlowed(in: VertexOutput, c: vec4f) -> vec4f { return vec4f(c.rgb + in.crowdGlow.rgb * select(0.8, 2.0, frame.toneMap == 4u), c.a); }
`}const sceneCrowdGPUResources=new WeakMap;function sceneCrowdGPUStatic(e,t){let o=sceneCrowdGPUResources.get(e);return o&&o.fanHeight===t||(o&&sceneCrowdGPUDispose(e),o={crowds:new Map,pipelines:new Map,fanHeight:t},o.bake=sceneCrowdBake(t),o.palette=e.createTexture({label:"gosx-crowd-palette",size:[o.bake.width,o.bake.height,1],format:"rgba32float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),e.queue.writeTexture({texture:o.palette},o.bake.data,{bytesPerRow:o.bake.width*16,rowsPerImage:o.bake.height},[o.bake.width,o.bake.height,1]),o.paletteView=o.palette.createView(),o.meshes=sceneCrowdMeshes(t).map(function(n){const a=e.createBuffer({label:"gosx-crowd-mesh",size:Math.ceil(n.vertices.byteLength/4)*4,usage:GPUBufferUsage.VERTEX|GPUBufferUsage.COPY_DST});e.queue.writeBuffer(a,0,n.vertices);const r=n.indices.length%2?Uint16Array.from(Array.from(n.indices).concat([0])):n.indices,i=e.createBuffer({label:"gosx-crowd-index",size:r.byteLength,usage:GPUBufferUsage.INDEX|GPUBufferUsage.COPY_DST});return e.queue.writeBuffer(i,0,r),{vbo:a,ibo:i,indexCount:n.indexCount}}),o.white=e.createTexture({label:"gosx-crowd-white",size:[1,1,1],format:"rgba8unorm",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),e.queue.writeTexture({texture:o.white},new Uint8Array([255,255,255,255]),{bytesPerRow:4},[1,1,1]),o.whiteView=o.white.createView(),o.sampler=e.createSampler({magFilter:"linear",minFilter:"linear"}),o.slot=e.createBuffer({label:"gosx-crowd-object",size:256,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),o.layout=e.createBindGroupLayout({label:"gosx-crowd",entries:[{binding:0,visibility:GPUShaderStage.VERTEX,buffer:{type:"uniform"}},{binding:1,visibility:GPUShaderStage.VERTEX,texture:{sampleType:"unfilterable-float"}},{binding:2,visibility:GPUShaderStage.VERTEX,texture:{sampleType:"unfilterable-float"}}]}),sceneCrowdGPUResources.set(e,o)),o}function sceneCrowdGPUDisposeCrowd(e){for(const t of["instances","uniform","sections","material"])e[t]&&e[t].destroy(),e[t]=null}function sceneCrowdGPUDispose(e){const t=sceneCrowdGPUResources.get(e);if(t){sceneCrowdGPUResources.delete(e);for(const o of t.crowds.values())sceneCrowdGPUDisposeCrowd(o);for(const o of t.meshes||[])o.vbo.destroy(),o.ibo.destroy();for(const o of["palette","white","slot"])t[o]&&t[o].destroy()}}function sceneCrowdGPUCrowd(e,t,o,n,a){let r=e.crowds.get(o.id);if(!r||r.ir!==o){r&&sceneCrowdGPUDisposeCrowd(r);const s=sceneCrowdPack(o);if(s.error)throw new Error(s.error);r={ir:o,packed:s,model:sceneCrowdModel(o),version:-1,uniforms:new Float32Array(SCENE_CROWD_UNIFORM_VEC4S*4)},r.instances=t.createBuffer({label:"gosx-crowd-fans",size:Math.max(32,s.records.byteLength),usage:GPUBufferUsage.VERTEX|GPUBufferUsage.COPY_DST}),t.queue.writeBuffer(r.instances,0,s.records),r.uniform=t.createBuffer({label:"gosx-crowd-uniform",size:r.uniforms.byteLength,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),r.sections=t.createTexture({label:"gosx-crowd-sections",size:[SCENE_CROWD_SECTION_TEXELS,o.sections.length,1],format:"rgba32float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),r.group=t.createBindGroup({label:"gosx-crowd",layout:e.layout,entries:[{binding:0,resource:{buffer:r.uniform}},{binding:1,resource:e.paletteView},{binding:2,resource:r.sections.createView()}]}),n.fans=s.sectionFans,e.crowds.set(o.id,r)}r.version!==n.version&&(t.queue.writeTexture({texture:r.sections},n.sections,{bytesPerRow:SCENE_CROWD_SECTION_TEXELS*16,rowsPerImage:o.sections.length},[SCENE_CROWD_SECTION_TEXELS,o.sections.length,1]),r.version=n.version);const i=a.material({color:"#ffffff",roughness:Number(o.material&&o.material.roughness)||.85,metalness:0},o.receiveShadow!==!1),c=i&&i.data;if((!r.material||r.materialSize!==c.byteLength)&&(r.material&&r.material.destroy(),r.material=t.createBuffer({label:"gosx-crowd-material",size:Math.max(256,c.byteLength),usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),r.materialSize=c.byteLength,r.materialGroup=null),t.queue.writeBuffer(r.material,0,c),!r.materialGroup||r.materialLayout!==a.materialLayout){const s=[{binding:0,resource:{buffer:r.material}}];for(let l=0;l<9;l++)s.push({binding:2*l+1,resource:e.whiteView},{binding:2*l+2,resource:e.sampler});s.push({binding:19,resource:{buffer:e.slot,size:112}}),r.materialGroup=t.createBindGroup({label:"gosx-crowd-material",layout:a.materialLayout,entries:s}),r.materialLayout=a.materialLayout}return r}function sceneCrowdGPUPipeline(e,t,o){const n=o+"|"+t.format+"|"+t.samples+"|"+(t.specular?1:0);let a=e.pipelines.get(n);if(a)return a;const r=t.device;(!e.vertexModule||e.vertexSource!==t.frameStructs)&&(e.vertexModule=r.createShaderModule({label:"gosx-crowd-vert",code:sceneCrowdVertexWGSL(t.frameStructs)}),e.vertexSource=t.frameStructs),(!e.fragmentModule||e.fragmentSource!==t.fragment)&&(e.fragmentModule=r.createShaderModule({label:"gosx-crowd-frag",code:sceneCrowdFragmentWGSL(t.fragment)}),e.fragmentSource=t.fragment);const i=t.specular?[{format:t.format},{format:"rgba16float"}]:[{format:t.format}],c={label:"gosx-crowd-"+o,layout:r.createPipelineLayout({bindGroupLayouts:[t.frameLayout,t.materialLayout,e.layout]}),vertex:{module:e.vertexModule,entryPoint:"vertexMain",constants:{CROWD_TIER:o},buffers:[{arrayStride:SCENE_CROWD_VERTEX_BYTES,stepMode:"vertex",attributes:[{shaderLocation:0,offset:0,format:"float32x3"},{shaderLocation:1,offset:12,format:"float32x3"},{shaderLocation:2,offset:24,format:"uint8x4"},{shaderLocation:3,offset:28,format:"unorm8x4"},{shaderLocation:4,offset:32,format:"uint8x4"}]},{arrayStride:32,stepMode:"instance",attributes:[{shaderLocation:5,offset:0,format:"float32x4"},{shaderLocation:6,offset:16,format:"float32"},{shaderLocation:7,offset:20,format:"uint16x2"},{shaderLocation:8,offset:24,format:"uint8x4"},{shaderLocation:9,offset:28,format:"uint8x4"}]}]},fragment:{module:e.fragmentModule,entryPoint:t.specular?"fragmentMainSpecular":"fragmentMain",targets:i},primitive:{topology:"triangle-list",cullMode:"back",frontFace:"ccw"},multisample:{count:Math.max(1,t.samples|0)},depthStencil:{format:"depth24plus",depthWriteEnabled:!0,depthCompare:"less-equal"}};return a=typeof t.create=="function"?t.create(r,"render",c):r.createRenderPipeline(c),e.pipelines.set(n,a),a}function sceneCrowdGPUDraw(e,t){const o=t.environment&&Array.isArray(t.environment.crowds)?t.environment.crowds:null,n={state:"none",fans:0,full:0,low:0,impostor:0,draws:0,cpuMs:0,error:""},a=typeof performance<"u"?performance.now():0;try{if(!o||!o.length)return t.device&&sceneCrowdGPUResources.has(t.device)&&sceneCrowdGPUDispose(t.device),n.state;const r=Number(t.time)||0;sceneCrowdNoteClock(t.mount,r);const i=new Set;n.state="ready";for(const s of o){if(!s||!s.id)continue;i.add(s.id);const l=sceneCrowdStore(t.mount,s);l.lastNow=r;const f=sceneCrowdGPUStatic(t.device,Number(s.fan&&s.fan.height)||1.75),u=sceneCrowdGPUCrowd(f,t.device,s,l,t),d=sceneCrowdSelect(u,u.packed,s,t);n.fans+=u.packed.count,n.full+=d.fans[0],n.low+=d.fans[1],n.impostor+=d.fans[2],t.device.queue.writeBuffer(u.uniform,0,sceneCrowdUniforms(s,l,f.bake,u.model,r,u.uniforms));for(let x=0;x<3;x++){const h=d.ranges[x];if(!h.length)continue;const v=sceneCrowdGPUPipeline(f,t,x),_=t.validation&&t.validation.get(v);if(_&&!_.ready){n.state="pending";continue}const p=f.meshes[x];e.setPipeline(_&&_.pipeline||v),e.setBindGroup(0,t.frameGroup),e.setBindGroup(1,u.materialGroup,[0]),e.setBindGroup(2,u.group),e.setVertexBuffer(0,p.vbo),e.setVertexBuffer(1,u.instances),e.setIndexBuffer(p.ibo,"uint16");for(let w=0;w<h.length;w+=2)e.drawIndexed(p.indexCount,h[w+1],0,0,h[w]),n.draws++}}const c=sceneCrowdGPUResources.get(t.device);if(c)for(const s of Array.from(c.crowds.keys()))i.has(s)||(sceneCrowdGPUDisposeCrowd(c.crowds.get(s)),c.crowds.delete(s))}catch(r){n.state="failed",n.error=String(r&&r.message||r)}finally{n.cpuMs=typeof performance<"u"?performance.now()-a:0,sceneCrowdPublish(t.mount,n)}return n.state}typeof window<"u"&&(window.__gosx_scene3d_crowd_api={webgl:sceneCrowdGLDraw,webgpu:sceneCrowdGPUDraw,dispose:function(e){e&&(sceneCrowdGLDispose(e),sceneCrowdGPUDispose(e))},apply:sceneCrowdApply,decode:sceneCrowdDecode,levels:sceneCrowdLevels,layout:sceneCrowdLayout,pack:sceneCrowdPack,bake:sceneCrowdBake,meshes:sceneCrowdMeshes,select:sceneCrowdSelect,actions:SCENE_CROWD_ACTIONS.map(function(e){return e[0]}),stats:null});
