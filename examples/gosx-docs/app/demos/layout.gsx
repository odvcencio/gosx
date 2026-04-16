package docs

// Active-state strategy: inline script.
// The layout has no server-side access to the current request path — layouts
// do not have their own DataLoader and data is owned by the child page. Rather
// than plumbing a layout.server.go for a single attribute, a small inline
// script reads location.pathname on first paint and:
//   1. sets aria-current="page" on the matching dock item, and
//   2. sets data-demo-slug on .demos-shell for per-demo CSS accent overrides.
// This degrades gracefully at prerender (no active state), corrected on first
// JS tick. No external file needed.

func Layout() Node {
    return <div class="demos-shell">
    	<header class="demos-topbar">
    		<span class="demos-topbar__crumb">
    			<a href="/" class="demos-topbar__home" data-gosx-link="true">GoSX</a>
    			<span class="demos-topbar__sep" aria-hidden="true">/</span>
    			<a href="/demos" class="demos-topbar__section" data-gosx-link="true">Demos</a>
    		</span>
    		<button
    			class="demos-topbar__menu"
    			type="button"
    			aria-label="Toggle demos menu"
    			aria-controls="demo-dock"
    			aria-expanded="false"
    		>
    			<span class="demos-topbar__menu-bar" aria-hidden="true"></span>
    			<span class="demos-topbar__menu-bar" aria-hidden="true"></span>
    			<span class="demos-topbar__menu-bar" aria-hidden="true"></span>
    		</button>
    	</header>
    	<div class="demos-body">
    		<nav id="demo-dock" class="demo-dock" aria-label="Demos">
    			<ul class="demo-dock__list" role="list">
    				<li class="demo-dock__item" role="listitem">
    					<a href="/demos/playground" class="demo-dock__link" data-demo="playground">
    						<span class="demo-dock__dot" aria-hidden="true"></span>
    						<span class="demo-dock__body">
    							<span class="demo-dock__title">Playground</span>
    							<span class="demo-dock__tag">write .gsx live</span>
    						</span>
    						<span class="demo-dock__chip demo-dock__chip--live">Live</span>
    					</a>
    				</li>
    				<li class="demo-dock__item" role="listitem">
    					<a href="/demos/fluid" class="demo-dock__link" data-demo="fluid">
    						<span class="demo-dock__dot" aria-hidden="true"></span>
    						<span class="demo-dock__body">
    							<span class="demo-dock__title">Fluid</span>
    							<span class="demo-dock__tag">server-streamed velocity field</span>
    						</span>
    						<span class="demo-dock__chip demo-dock__chip--live">Live</span>
    					</a>
    				</li>
    				<li class="demo-dock__item" role="listitem">
    					<a href="/demos/livesim" class="demo-dock__link" data-demo="livesim">
    						<span class="demo-dock__dot" aria-hidden="true"></span>
    						<span class="demo-dock__body">
    							<span class="demo-dock__title">Live Sim</span>
    							<span class="demo-dock__tag">authoritative multiplayer</span>
    						</span>
    						<span class="demo-dock__chip demo-dock__chip--live">Live</span>
    					</a>
    				</li>
    				<li class="demo-dock__item" role="listitem">
    					<a href="/demos/cms" class="demo-dock__link" data-demo="cms">
    						<span class="demo-dock__dot" aria-hidden="true"></span>
    						<span class="demo-dock__body">
    							<span class="demo-dock__title">CMS</span>
    							<span class="demo-dock__tag">block editor</span>
    						</span>
    						<span class="demo-dock__chip demo-dock__chip--live">Live</span>
    					</a>
    				</li>
    				<li class="demo-dock__item" role="listitem">
    					<a href="/demos/scene3d" class="demo-dock__link" data-demo="scene3d">
    						<span class="demo-dock__dot" aria-hidden="true"></span>
    						<span class="demo-dock__body">
    							<span class="demo-dock__title">Scene3D</span>
    							<span class="demo-dock__tag">PBR showroom</span>
    						</span>
    						<span class="demo-dock__chip demo-dock__chip--live">Live</span>
    					</a>
    				</li>
    				<li class="demo-dock__item" role="listitem">
    					<a href="/demos/scene3d-bench" class="demo-dock__link" data-demo="scene3d-bench">
    						<span class="demo-dock__dot" aria-hidden="true"></span>
    						<span class="demo-dock__body">
    							<span class="demo-dock__title">Scene3D Bench</span>
    							<span class="demo-dock__tag">frame-time instrumentation</span>
    						</span>
    						<span class="demo-dock__chip demo-dock__chip--live">Live</span>
    					</a>
    				</li>
    				<li class="demo-dock__item" role="listitem">
    					<a href="/demos/collab" class="demo-dock__link" data-demo="collab">
    						<span class="demo-dock__dot" aria-hidden="true"></span>
    						<span class="demo-dock__body">
    							<span class="demo-dock__title">Collab Editor</span>
    							<span class="demo-dock__tag">LWW markdown sync</span>
    						</span>
    						<span class="demo-dock__chip demo-dock__chip--live">Live</span>
    					</a>
    				</li>
    			</ul>
    		</nav>
    		<div class="demo-viewport">
    			<Slot />
    			<footer class="demo-meta" role="contentinfo" aria-label="Demo metadata">
    				<button type="button" class="demo-meta__pill" data-drawer="source">Source</button>
    				<button type="button" class="demo-meta__pill" data-drawer="packages">Packages used</button>
    				<button type="button" class="demo-meta__pill" data-drawer="prerender">Prerender status</button>
    			</footer>
    		</div>
    	</div>
    	<script>
    		{`(function(){var p=location.pathname.replace(/\/$/,'');var s=p.split('/demos/')[1];if(!s)return;var shell=document.querySelector('.demos-shell');if(shell)shell.setAttribute('data-demo-slug',s);var lnk=document.querySelector('.demo-dock__link[data-demo="'+s+'"]');if(!lnk)return;var li=lnk.closest('li');if(li)li.setAttribute('aria-current','page');lnk.setAttribute('aria-current','page');})()`}
    	</script>
    </div>
}
