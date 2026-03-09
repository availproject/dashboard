# Spec Questions & Answers

1. Sprint week detection
The spec says the dashboard shows "week N of M" — how does the tool know which week it
currently is? Does the user set a sprint start date in Config, or is it parsed from the
Notion document? What if different teams have staggered start dates?

A: the notion document should specify it, and if it's not there the tool should warn

2. Business metrics connectivity
"User pastes a dashboard URL, selects panels" — does the user also supply API keys/tokens
for Grafana/PostHog/SigNoz? These APIs require auth. Is credential management part of the
Config section, or is this read from public/embedded dashboards?

A: can be specified in .env file on the server

3. User management
The spec mentions an auth system with view/edit roles. Who provisions users? Is there a
self-registration flow, or does an admin invite people? Is this a single-tenant install
(one org per deployment), or could it be multi-tenant?

A: single tenant, simple config with hashed passwords in the same database used for caching data. admin adds users in the config.

4. Marketing velocity
The spec says non-developer velocity is deprioritized "unless a clear signal exists" —
then immediately describes marketing's Notion database as a clear signal. Should marketing
 velocity be a first-class feature, or should it remain out of scope for the initial
build?

A: can be added in a later phase

5. Snooze
The annotation snooze is marked "(Optional, if implemented)." Is it in scope or out? It
affects the annotation data model.

A: snooze is not needed

6. Cross-team workload in the team view
When showing workload for Team A, does "Alice: ~120% capacity" reflect only Alice's work
assigned through Team A's GitHub project, or does it include Alice's assignments across
all teams? The org-level view aggregates cross-team — but what does the team-level view
show?

A: let's standardize on work-days across the board. So in a team it might show Alice: 3.5 (3.5 days of work planned/assigned/inferred), and at the org-level it might show Alice: 5 (5 days of work planned/assigned/inferred)

7. Plan rollover for open items
When a user re-tags documents at rollover, what happens to open GitHub issues from the old
 plan that weren't completed? Are they just picked up naturally because they're still
open, or is there explicit tracking of "carried over" items?

A: our workflow is to never roll forward github tasks from one sprint to another. We ALWAYS close tasks at the end of the sprint as Done, Not Completed, or Won't Do. If we still want to do a task in a future sprint, we make a new one, with the same or different scope.

8. Discovery re-runs
When the user re-runs discovery on an already-configured source, should it: (a) only
surface newly found items, leaving existing tags intact, or (b) reset and show everything
again? This matters for the UX of the Config section when sources grow.

A: It should merge new data, leaving existing config intact, so that the user doesn't have to re-tag/re-configure everything. For example, if doc A is used for goals and doc B is ignored, and after re-discovery doc C is discovered, it should be analyzed to determine what it might be good for, and automatically configured for that. But A should remain for goals, and B should remain ignored, since they were previously configured.
