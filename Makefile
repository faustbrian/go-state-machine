GOLIB ?= golib

.PHONY: check check-full ci cohesion inventory repository-check workflows

check:
	$(GOLIB) check --local --all

check-full:
	$(GOLIB) specification check --online
	$(GOLIB) check --all

ci:
	$(GOLIB) repository check
	$(GOLIB) cohesion check
	$(GOLIB) check --local --all

cohesion:
	$(GOLIB) cohesion check

inventory repository-check:
	$(GOLIB) repository check

workflows:
	$(GOLIB) workflows check
