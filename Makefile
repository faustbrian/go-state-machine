GOLIB ?= golib

.PHONY: source-docs-check check check-full ci cohesion inventory repository-check workflows

check: source-docs-check
	$(GOLIB) check --local --all

check-full: source-docs-check
	$(GOLIB) specification check --online
	$(GOLIB) check --all

ci: source-docs-check
	$(GOLIB) repository check
	$(GOLIB) cohesion check
	$(GOLIB) check --local --all

cohesion:
	$(GOLIB) cohesion check

inventory repository-check:
	$(GOLIB) repository check

workflows:
	$(GOLIB) workflows check

source-docs-check:
	$(GOLIB) source-docs check
