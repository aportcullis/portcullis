include project.mk

.PHONY: verify-progress
verify-progress:
	"$(VERIFICATION_NODE)" "$(VERIFICATION_PROGRESS_CHILD)"
