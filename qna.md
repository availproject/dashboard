# Q&A

Data & Sources
1. Beyond Notion and GitHub, are there other platforms in your current stack? (Linear,
Jira, Slack, analytics tools, CRM, etc.) What are the key business metrics platforms you'd
 want to pull from?

A: We use GitHub for tasks, code, and some documents. We use notion for documents (goals, sprint plans, ideas, etc), and some teams also use it for tasks (marketing in particular). For business metrics we use signoz, posthog, and grafana.

2. For Notion specifically — do your teams currently use a consistent structure (e.g.,
sprint plans always in a specific database template), or is it ad-hoc per team?

A: It's ad-hoc at the moment. Goals and sprint plans are text (not databases, not structured). Some teams have more structure: marketing uses a database where rows are campaigns or multi-week sprints, and contain a field with tasks modeled as documents, each with their own text body and metadata.

3. Are sprint plans currently written in a specific format, or would teams adopt a new
format to support this tool?

A: Sprint plans are text, and will likely stay that way. Sprint tasks are added to github projects/issues and are more structured there.

Concerns & AI Reasoning
4. For the AI-generated concerns — when the AI flags something (e.g., "inter-team
dependency at risk"), do you want to be able to dismiss it, snooze it, or annotate it with
 your own context? Or is it purely read-only output?

A: Yes I want to dismiss, annotate, add my own context to guide the AI by adding additional context / correcting misconceptions.

5. How much do you trust AI-generated assessments vs. wanting to validate them? (This
shapes how prominent warnings/concerns should be vs. raw data)

A: As per the previous question, I will iterate with the dashboard so that my understanding of reality matches what the dashboard is saying. In general, warnings and conerns should have explanations, so that I can either address the issue or provide more context.

Resource & Effort Tracking
6. For employee effort/workload — do you currently track allocations explicitly (e.g.,
"Alice is 50% on Team A this sprint") anywhere, or should this be entirely inferred from
activity signals?

A: Entirely inferred.

7. When you say "not backward-looking" — if someone hasn't committed code yet this sprint
but has open tasks assigned, should that count as current effort?

A: Yes. This dashboard is not meant as an employee performance evaluation tool. The main goal is to know if someone is overloaded on paper -- do they have too much work assigned? In some cases people do work that is not properly tracked in tasks, so it's good to get a holistic picture based on commits as well. But the goal is to estimate workload.

Feedback Loop
8. When you say "tweak sources to give feedback, reload dashboard" — what does tweaking
look like? Adding/removing source documents? Annotating that the AI got something wrong?
Something else?

A: This is a key question, related to #4 above. I can see three possibilities:
- a way to add additional context directly in the tool. dismiss an item, add a note, etc
- go back to the source documents and tweak them to see if the output changes
- have some specific additional document that can be used for the AI and human to do some kind of Q&A (like we're doing here)

Of these, I think directly in the tool is the best workflow, but I worry a bit that the tool will need to store state that isn't a cache of anything else, it's additional concext. Still, probably worth it. Something like, in the goals, I can select a goal and say "this is not at risk because a, b, c" and that gets passed on to the AI as additional context on the next sync.

9. If the AI says "Team X is on track" but you think it's at risk — can you override that
assessment, and should your override persist and influence future AI runs?

A: Yes I would want to provide more context etc until the AI and I converge on the assessment.

Users & Scope
10. Who are the intended users? Just you, or would team leads also have access to view (or
 edit) their team's section?

A: Leads. No need for fine grained access control, though. Maybe just 2 levels: view and edit. If they have edit, they can provide context, configure, etc. Otherwise they can only view. Globally in both cases, no need to track this per team or anything like that.

11. Roughly how many teams and total employees are we talking about? (Affects dashboard
layout decisions)

A: About 25-50 people total. 4 teams.

Sprints & Planning
12. Can a team run multiple 4-week plans concurrently (different workstreams), or is it
always one active plan at a time?

A: Each team has their own 4-week plan and separate sprint, etc. Parallel workstreams.

13. The "additional sprints to complete original goals" — should the tool track this as
plan slippage, or is it just neutral (extended timeline)?

A: Tool just needs to account for this reality. I need the tool to be aware of where in the plan a given team is (e.g. week 3 of 4), and if we need to add a 5th sprint, the tool needs to roll with that. Also when we do that it will necessarily delay the next 4-week plan, which can introduce risks (and can be flagged as such).

TUI Navigation
14. How do you envision the TUI structure — e.g., a main org-level view with the ability to
 drill into a team, then drill into specific items (goals, concerns, people)? Or more of a
 scrollable single-page layout? This shapes the whole interaction model.

A: Drill-down

15. For the feedback/annotation workflow in the TUI — do you imagine this as inline editing
 (cursor to an item, press a key to annotate), or a separate "edit context" mode?

A: Inline, plus a section in the config where all annotations are aggregated so that there is one place to view and edit all of them (otherwise it might be hard to know what is being sent additionally to the model and it might grow unbounded)

Business Metrics
16. For SigNoz/PostHog/Grafana — are the specific metrics you want to surface configurable
per-dashboard, or do you have a fixed set in mind (e.g., error rate, DAU, latency)? I want
 to understand how opinionated the tool should be here vs. fully flexible.

A: Will depend on each product/team, so it needs to be flexible

Alerts & Notifications
17. Should the dashboard proactively notify you when something newly becomes at risk (e.g.,
 a flag appears since last load), or is it purely passive — you come to it, it shows
current state?

A: Passive, after a refresh it shows the current state

Velocity
18. For non-developer teams like marketing, how would you define velocity? Task/campaign
completion rate from Notion? Or is this something the user configures per team?

A: Maybe we can punt on velocity for those teams, if there isn't some clear way to do it? Don't feel strongly about it.

Sync
19. What's an acceptable data staleness? Is a daily scheduled sync fine, or do you need
on-demand refresh to be near-real-time (e.g., pulling latest GitHub commits immediately)?

A: On-demand sync, incremental whenever possible, and limited to only what is needed. There needs to be some initial/less frequent discovery step where sources are analyzed and after being catalogued and configured we only pull what is needed. For example, we might have 50 documents in notion, revealed during discovery. but there needs to be a way to indicate which documents should be used vs ignored, and for which sections (e.g. use document A to determine goals, but use B to determine something else). Then during sync it would only check for changes to documents A and B, since the rest are ignored/not being used.

Similarly for sprint tasks on github, we need to design a way to fetch tasks incrementally and quickly. We use github projects, but project tasks are also issues and tagged with each product area, so we could use the issues search API with tags for example. It's important that after initial/out of band discovery and analysis, the regular sync step be fast, because it's in the critical path when I load, give context, reload.

Goals & Milestones
20. Org-level goals and milestones — where do these live currently? Also in Notion? And is
there an explicit hierarchy (org goal → team goal → sprint task) or is that connection
left for the AI to infer?

A: We will create a page in notion for this, similar to what we do for teams. There is no explicit hierarchy, but part of the analysis we would want from the model is to highlight if there is misalignment between org level goals and team ones. Org level goals will be on longer time horizons than team ones. So we will want to know whether the team goals are aligned directionally.

Discovery & Configuration Flow
21. When discovery runs on a new source (say, a Notion workspace), you'd get a list of
catalogued documents/databases. How do you envision "tagging" them for use — e.g., marking
 a doc as "Team A's sprint plan" vs "Team A's goals"? Is this a one-time setup you do in a
 config section, or something you redo when sources change?

A: It should be a section in the config with the currently discovered sources so I can configure what will get used for what purpose, and then I would only return to it if/when something changes structurally (a document is added, or moved for example). It should not be a wizard, it should be a section or several sections.

Annotation Lifecycle
22. Annotations guide the AI on future syncs. What happens when the underlying situation
resolves — does the annotation auto-expire, or do you manually clean it up? (Relevant to
the "growing unbounded" concern you mentioned.)

A: If it's possible for the AI to automatically tell that circumstances have changed, then it would be nice to have that flagged somehow, but that's a nice-to-have, not essential. I expect to manually clean up the guidance from time to time.

Dashboard Freshness UX
23. When you open the dashboard, do you want to see the last cached state immediately (with
 a timestamp + refresh button), or is it always blank until a sync runs? What's the
experience when a sync is in progress?

A: I would want to see the latest cached data. During sync there should be a banner/notice (could be modal) that sync is in progress until it completes.

Concerns Persistence
24. If you dismiss a concern ("this is not at risk because X") and then on the next sync
the AI flags the same issue again — should it stay dismissed until you explicitly
un-dismiss, or should it resurface with the new evidence?

A: My expectation is that when I give additional context "this is not a risk because X", this gets passed on to the model so that it would not flag the same issue again. When issues are flagged, they always need an explanation for why it is being flagged, and our prompt should explicitly ask the AI to double check with the additional guidance.

Upcoming Plans
25. Should the tool be aware of the next 4-week plan (not yet started) — e.g., to flag that
 a delayed current plan will push out the next one? Or is forward-looking limited to the
current plan's remaining sprints?

A: Yes it should be aware of this. We will have the next 4-week plan in notion as a separate doc, so we should think about what the workflow will be to point the tool at the new plan and let it know that it is the _next_ sprint plan, not the current one.

Annotations & lifecycle:
26. Annotations are tied to specific items (a goal, a concern). When a sprint ends and a
new plan starts, what happens to annotations on the old items — do they expire, or should
some carry forward? (E.g., an annotation about a team's general working style might still
be relevant next sprint.)

A: Let's have two tiers of annotations: per-item, and per-team (or org for the org level dashboard). Per item annotations are automatically archived once no longer relevant, vs general annotations survive until manually deleted/edited.

Workload:
27. When the dashboard surfaces that someone is overloaded, what's the expected output format? A qualitative signal ("Alice: high load across 3 teams"), a rough quantitative estimate ("Alice: ~140% capacity"), or something else? This affects how the AI is prompted to reason about it.

A: I think something like a percentage or maybe a team section with a simple count of how many days of work someone is estimated to have in a sprint (based on analysis of how long tasks would take to complete, effort for committed code, PRs, etc)

Inter-team dependencies:
28. Are inter-team dependencies ever explicitly written down (e.g., in a sprint plan doc: "Team B needs Team A's API before week 3"), or are they always implicit and left for the AI to infer from context?

A: No, they aren't, but let's not worry about inter-team dependencies yet and first focus on getting the dashboard up and running.

Business metrics:
29. For SigNoz/PostHog/Grafana — how do you imagine configuring which metrics to show? For
example: pasting a dashboard/panel URL, writing a metric query, or specifying named metrics that the tool knows how to fetch?

A: Ideal would be to paste a dashboard URL and get a list of panels to choose from

Velocity (developers):
30. For developer velocity charts from GitHub — what signals matter most to you? PR merge rate, issues closed, commit frequency, or something else?

A: It all matters. I want to estimate the difficulty of all actions (tasks, PRs, commits) and then estimate how much work is getting done per sprint, so that we can better estimate how much work we can do in future sprints. For tasks, it is easy because we never roll over tasks: we always close tasks at the end of each sprint as done / not completed / won't do, and if not completed we choose whether to make a new task in a future sprint (with the same or different scope).

Sprint plan rollover:
31. When a 4-week plan ends and a new one begins, does the old plan just get archived (still viewable as history), or do you only ever care about the current + next plan?

A: I only really care about current + next. previous plans will be archived in notion, but the tool doesn't need to care about them.
