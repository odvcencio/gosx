#!/usr/bin/env sh

# Confirm that the Deployment still represents the release observed before a
# build began. Resource version is intentionally excluded here because status
# and controller annotations can change during a long build without changing
# release ownership. The short get-to-patch transaction fences those writes.
gosx_docs_deployment_matches_base() {
	printf '%s\n' "$1" | jq -e \
		--arg uid "$2" \
		--argjson generation "$3" \
		--argjson spec "$4" \
		--argjson changeCausePresent "$5" \
		--arg changeCause "$6" \
		--argjson transactionPresent "$7" \
		--arg transaction "$8" \
		'def annotation_matches($key; $present; $value):
			if $present then .metadata.annotations[$key] == $value
			else ((.metadata.annotations // {}) | has($key) | not)
			end;
		.metadata.uid == $uid and
		.metadata.generation == $generation and
		.spec == $spec and
		annotation_matches("kubernetes.io/change-cause"; $changeCausePresent; $changeCause) and
		annotation_matches("gosx.m31labs.dev/deploy-transaction"; $transactionPresent; $transaction)' >/dev/null
}

# Classify live state while recovering from an ambiguous API response. The
# per-invocation transaction ID prevents two identical same-SHA releases from
# claiming one another. Any state other than our exact base/release is foreign.
gosx_docs_deployment_owner() {
	printf '%s\n' "$1" | jq -er \
		--arg uid "$2" \
		--argjson previousSpec "$3" \
		--argjson previousChangeCausePresent "$4" \
		--arg previousChangeCause "$5" \
		--argjson previousTransactionPresent "$6" \
		--arg previousTransaction "$7" \
		--argjson releaseSpec "$8" \
		--arg releaseChangeCause "$9" \
		--arg releaseTransaction "${10}" \
		'def annotation_matches($key; $present; $value):
			if $present then .metadata.annotations[$key] == $value
			else ((.metadata.annotations // {}) | has($key) | not)
			end;
		if .metadata.uid != $uid then "other"
		elif .spec == $previousSpec and
			annotation_matches("kubernetes.io/change-cause"; $previousChangeCausePresent; $previousChangeCause) and
			annotation_matches("gosx.m31labs.dev/deploy-transaction"; $previousTransactionPresent; $previousTransaction)
		then "base"
		elif .spec == $releaseSpec and
			.metadata.annotations["kubernetes.io/change-cause"] == $releaseChangeCause and
			.metadata.annotations["gosx.m31labs.dev/deploy-transaction"] == $releaseTransaction
		then "release"
		else "other"
		end'
}

# Build the optimistic transaction that installs a release Deployment.
gosx_docs_release_patch() {
	jq -cn \
		--arg uid "$1" \
		--arg resourceVersion "$2" \
		--argjson generation "$3" \
		--argjson previousSpec "$4" \
		--argjson previousTemplate "$5" \
		--argjson releaseSpec "$6" \
		--arg releaseChangeCause "$7" \
		--arg releaseTransaction "$8" \
		'[
			{"op":"test","path":"/metadata/uid","value":$uid},
			{"op":"test","path":"/metadata/resourceVersion","value":$resourceVersion},
			{"op":"test","path":"/metadata/generation","value":$generation},
			{"op":"test","path":"/spec","value":$previousSpec},
			{"op":"test","path":"/spec/template","value":$previousTemplate},
			{"op":"replace","path":"/spec","value":$releaseSpec},
			{"op":"add","path":"/metadata/annotations/kubernetes.io~1change-cause","value":$releaseChangeCause},
			{"op":"add","path":"/metadata/annotations/gosx.m31labs.dev~1deploy-transaction","value":$releaseTransaction}
		]'
}

# Atomically invalidate a pending release request that still targets the base
# resourceVersion. If this wins, the original patch cannot commit later.
gosx_docs_recovery_fence_patch() {
	jq -cn \
		--arg uid "$1" \
		--arg resourceVersion "$2" \
		--argjson generation "$3" \
		--argjson baseSpec "$4" \
		--argjson baseTemplate "$5" \
		--argjson changeCausePresent "$6" \
		--arg changeCause "$7" \
		--argjson transactionPresent "$8" \
		--arg transaction "$9" \
		--arg recoveryFence "${10}" \
		'[
			{"op":"test","path":"/metadata/uid","value":$uid},
			{"op":"test","path":"/metadata/resourceVersion","value":$resourceVersion},
			{"op":"test","path":"/metadata/generation","value":$generation},
			{"op":"test","path":"/spec","value":$baseSpec},
			{"op":"test","path":"/spec/template","value":$baseTemplate}
		]
		+ (if $changeCausePresent then [{"op":"test","path":"/metadata/annotations/kubernetes.io~1change-cause","value":$changeCause}] else [] end)
		+ (if $transactionPresent then [{"op":"test","path":"/metadata/annotations/gosx.m31labs.dev~1deploy-transaction","value":$transaction}] else [] end)
		+ [{"op":"add","path":"/metadata/annotations/gosx.m31labs.dev~1recovery-fence","value":$recoveryFence}]'
}

# Restore the captured spec only while this unique release still owns it.
gosx_docs_rollback_patch() {
	jq -cn \
		--arg uid "$1" \
		--argjson generation "$2" \
		--argjson releaseSpec "$3" \
		--argjson releaseTemplate "$4" \
		--arg releaseChangeCause "$5" \
		--arg releaseTransaction "$6" \
		--argjson previousSpec "$7" \
		--argjson previousChangeCausePresent "$8" \
		--arg previousChangeCause "$9" \
		--argjson previousTransactionPresent "${10}" \
		--arg previousTransaction "${11}" \
		'[
			{"op":"test","path":"/metadata/uid","value":$uid},
			{"op":"test","path":"/metadata/generation","value":$generation},
			{"op":"test","path":"/spec","value":$releaseSpec},
			{"op":"test","path":"/spec/template","value":$releaseTemplate},
			{"op":"test","path":"/metadata/annotations/kubernetes.io~1change-cause","value":$releaseChangeCause},
			{"op":"test","path":"/metadata/annotations/gosx.m31labs.dev~1deploy-transaction","value":$releaseTransaction},
			{"op":"replace","path":"/spec","value":$previousSpec}
		]
		+ if $previousChangeCausePresent then
			[{"op":"add","path":"/metadata/annotations/kubernetes.io~1change-cause","value":$previousChangeCause}]
		else
			[{"op":"remove","path":"/metadata/annotations/kubernetes.io~1change-cause"}]
		end
		+ if $previousTransactionPresent then
			[{"op":"add","path":"/metadata/annotations/gosx.m31labs.dev~1deploy-transaction","value":$previousTransaction}]
		else
			[{"op":"remove","path":"/metadata/annotations/gosx.m31labs.dev~1deploy-transaction"}]
		end'
}
