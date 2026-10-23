package docs

import (
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/signal"
)

func Page() Node {
	return <section
		class="vy"
		aria-label="Voyage"
		role="region"
		data-view={data.view}
		data-period={data.period}
		data-revealed="true"
		data-gosx-scene3d-control-scope
		data-gosx-scene3d-status-scope
	>
		<div class="vy__canvas gosx-scene3d-poster-stage" data-gosx-scene3d-poster-stage>
			<img
				class="gosx-scene3d-poster"
				src={data.posterSrc}
				alt=""
				width="1000"
				height="625"
				decoding="async"
				fetchpriority="high"
			 />
			<Scene3D {...data.scene} stats={false} />
		</div>
		<a class="vy__close" href="/demos" aria-label="Back to the demos">×</a>
		<div class="vy__controls">
			<VoyageControls State={data.state} Speed={data.speedID} View={data.view} />
		</div>
	</section>
}

//gosx:island
func VoyageControls(props struct{ State, Speed, View string }) Node {
	request := signal.NewShared("voyage.presentation", scene.PresentationRequest{})
	state := signal.NewShared("voyage.presentationState", scene.PresentationState{View: props.View, Period: props.State + "-" + props.Speed})
	sea := signal.NewShared("voyage.sea", props.State)
	speed := signal.NewShared("voyage.speed", props.Speed)
	selectSea := func() {
		sea.Set(eventData)
		request.Set(scene.PresentationRequest{Period: sea.Get() + "-" + speed.Get(), Revision: request.Get().Revision + 1})
	}
	selectSpeed := func() {
		speed.Set(eventData)
		request.Set(scene.PresentationRequest{Period: sea.Get() + "-" + speed.Get(), Revision: request.Get().Revision + 1})
	}
	selectView := func() { request.Set(scene.PresentationRequest{View: eventData, Revision: request.Get().Revision + 1}) }
	return <div class="vy__panel">
		<nav class="vy__group" aria-label="Sea state">
			<button type="button" class="vy__link" onClick={selectSea} data-gosx-event-value="calm" aria-pressed={sea.Get()=="calm"}>Calm</button>
			<button type="button" class="vy__link" onClick={selectSea} data-gosx-event-value="moderate" aria-pressed={sea.Get()=="moderate"}>Moderate</button>
			<button type="button" class="vy__link" onClick={selectSea} data-gosx-event-value="rough" aria-pressed={sea.Get()=="rough"}>Rough</button>
			<button type="button" class="vy__link" onClick={selectSea} data-gosx-event-value="storm" aria-pressed={sea.Get()=="storm"}>Storm</button>
		</nav>
		<nav class="vy__group" aria-label="Game speed">
			<button type="button" class="vy__link" onClick={selectSpeed} data-gosx-event-value="1x" aria-pressed={speed.Get()=="1x"}>1×</button>
			<button type="button" class="vy__link" onClick={selectSpeed} data-gosx-event-value="10x" aria-pressed={speed.Get()=="10x"}>10×</button>
			<button type="button" class="vy__link" onClick={selectSpeed} data-gosx-event-value="60x" aria-pressed={speed.Get()=="60x"}>60×</button>
			<button type="button" class="vy__link" onClick={selectSpeed} data-gosx-event-value="360x" aria-pressed={speed.Get()=="360x"}>360×</button>
			<button type="button" class="vy__link" onClick={selectSpeed} data-gosx-event-value="1000x" aria-pressed={speed.Get()=="1000x"}>1,000×</button>
		</nav>
		<nav class="vy__group" aria-label="View">
			<button type="button" class="vy__link" onClick={selectView} data-gosx-event-value="quarter" aria-pressed={state.Get().View=="quarter"}>Quarter</button>
			<button type="button" class="vy__link" onClick={selectView} data-gosx-event-value="waterline" aria-pressed={state.Get().View=="waterline"}>Waterline</button>
		</nav>
		<p class="vy__note">The ship holds its place; the water passes it at an apparent speed that rises with game speed, while the waves keep their natural pace.</p>
	</div>
}
