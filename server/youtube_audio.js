// gosx YouTube audio bridge — declarative background audio from YouTube.
//
// Served by the gosx server at /gosx/youtube-audio.js. Any element with
// data-gosx-youtube-audio="<youtube url>" becomes a play/pause toggle:
//   - First activation lazy-loads the YouTube IFrame API and creates one
//     hidden 1x1 player shared by the whole page.
//   - The active element carries data-gosx-youtube-audio-state="playing"
//     while audio plays; the attribute is removed on pause or track end.
//     Style the two states from CSS — the bridge never touches content.
//   - Activating a different element stops the current track first.
//
// The bridge loads nothing until the first activation, so pages that render
// no audio toggles pay only for this script's parse.
(function () {
  "use strict";

  var ATTR = "data-gosx-youtube-audio";
  var STATE_ATTR = "data-gosx-youtube-audio-state";

  var player = null;
  var currentVideoId = null;
  var apiReady = false;
  var pendingPlay = null;
  var activeButton = null;

  function extractVideoId(url) {
    if (!url) return null;
    var m = String(url).match(/(?:youtu\.be\/|youtube\.com\/(?:watch\?v=|embed\/|v\/))([a-zA-Z0-9_-]{11})/);
    return m ? m[1] : null;
  }

  function loadAPI() {
    if (document.getElementById("gosx-youtube-api-script")) return;
    var tag = document.createElement("script");
    tag.id = "gosx-youtube-api-script";
    tag.src = "https://www.youtube.com/iframe_api";
    document.head.appendChild(tag);
  }

  var previousAPIReady = window.onYouTubeIframeAPIReady;
  window.onYouTubeIframeAPIReady = function () {
    if (typeof previousAPIReady === "function") {
      try { previousAPIReady(); } catch (_e) { /* ignore */ }
    }
    apiReady = true;
    if (pendingPlay) {
      playVideo(pendingPlay.videoId, pendingPlay.button);
      pendingPlay = null;
    }
  };

  function ensureContainer() {
    var el = document.getElementById("gosx-youtube-audio-host");
    if (!el) {
      el = document.createElement("div");
      el.id = "gosx-youtube-audio-host";
      el.style.cssText = "position:fixed;top:-100px;left:-100px;width:1px;height:1px;overflow:hidden;pointer-events:none;";
      document.body.appendChild(el);
    }
    return el;
  }

  function setState(button, playing) {
    if (!button) return;
    if (playing) {
      button.setAttribute(STATE_ATTR, "playing");
      activeButton = button;
    } else {
      button.removeAttribute(STATE_ATTR);
      if (activeButton === button) activeButton = null;
    }
  }

  function clearActiveStates() {
    var active = document.querySelectorAll("[" + ATTR + "][" + STATE_ATTR + "]");
    for (var i = 0; i < active.length; i++) {
      active[i].removeAttribute(STATE_ATTR);
    }
    activeButton = null;
  }

  function playVideo(videoId, button) {
    if (!apiReady) {
      pendingPlay = { videoId: videoId, button: button };
      loadAPI();
      return;
    }

    var container = ensureContainer();

    if (player && currentVideoId === videoId) {
      var state = player.getPlayerState();
      if (state === YT.PlayerState.PLAYING) {
        player.pauseVideo();
        setState(button, false);
      } else {
        player.playVideo();
        setState(button, true);
      }
      return;
    }

    clearActiveStates();

    if (player) {
      player.destroy();
      player = null;
    }

    container.innerHTML = '<div id="gosx-youtube-audio-player"></div>';
    currentVideoId = videoId;

    player = new YT.Player("gosx-youtube-audio-player", {
      height: "1",
      width: "1",
      videoId: videoId,
      playerVars: { autoplay: 1, controls: 0, disablekb: 1, fs: 0, modestbranding: 1 },
      events: {
        onReady: function (event) {
          event.target.playVideo();
          setState(button, true);
        },
        onStateChange: function (event) {
          if (event.data === YT.PlayerState.ENDED) {
            setState(button, false);
            currentVideoId = null;
          }
        }
      }
    });
  }

  document.addEventListener("click", function (e) {
    var btn = e.target && e.target.closest ? e.target.closest("[" + ATTR + "]") : null;
    if (!btn) return;
    var videoId = extractVideoId(btn.getAttribute(ATTR));
    if (videoId) {
      playVideo(videoId, btn);
    }
  });
})();
