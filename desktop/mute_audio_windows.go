//go:build windows && (amd64 || arm64)

package desktop

// muteMediaAudioScript runs before each document's scripts. It covers media
// elements added later and calls to HTMLMediaElement.play, so a desktop host
// can be launched silently by smoke tests and kiosk apps.
const muteMediaAudioScript = `(function(){
  const mute = element => { if (element instanceof HTMLMediaElement) { element.muted = true; element.volume = 0; } };
  const play = HTMLMediaElement.prototype.play;
  HTMLMediaElement.prototype.play = function() { mute(this); return play.apply(this, arguments); };
  const observer = new MutationObserver(records => {
    for (const record of records) for (const node of record.addedNodes) {
      if (node instanceof HTMLMediaElement) mute(node);
      if (node.querySelectorAll) for (const media of node.querySelectorAll('audio,video')) mute(media);
    }
  });
  observer.observe(document, {childList:true, subtree:true});
  for (const media of document.querySelectorAll('audio,video')) mute(media);
})();` + "\n"
