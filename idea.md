# Idea: Product status dashboard

As head of product for a stall startup, I manage several teams working on concurrent
projects. In each team, we plan 1-week sprints following a 4-week plan. Sometimes we
add additional sprints to complete the original 4-week plan goals. At any given time,
 I want a dashboard that shows at a glance:

For each team/area:
- What a team is working on. Where they are on their 4-sprint plan. Goals for the
sprint and the 4-week plan. What is on track vs what is at risk.
- Any concerns, across the board. Dependencies not lining up. Something being stuck
and putting other things at risk. Under-funding causing something to move too slowly.
 Omissions in the plan. Inter-team dependencies not lining up. Missing plans to
achieve stated goals. Etc.
- Who is working on the project and the level of effort they are currently/scheduled
to be putting into it, deduced by inspecting open tasks, future tasks, github
commits, etc. (Employees contribute to multiple teams at the same time). Not
backward-looking, but current/forward focused. The idea is to highlight whether there
 is a mismatch between our resource allocation and what is needed to fulfill the plan
 on time.
- team velocity charts, calculated from github, notion, etc. For developers,
marketing, etc.
- business metrics, extracted from other platforms and summarized here

For the org as a whole:
- A summary of the team status for each team: what are they working towards, when is
their next milestone, is anything at risk/needing attention, etc
- An assessment of workload for each employee by looking at their work across the
org. Is someone overloaded with too many threads on different projects? Also in
future sprints: is someone expected to become overloaded as per plans? (so that we
can reshuffle work potentially)
- aggregate business intelligence metrics
- aggregate team velocity metrics
- org-level / business goals and milestones, with an assessment of whether goals are
on track, at risk, etc

Also a configuration section to configure everything (nothing should be hardcoded):
- which teams exist, team members, with metadata like github usernames, etc
- org-level documents (e.g., where in notion goals are described)
- for each team, where documents are stores (notion, github). multiple sources should
 be configurable. which parts of the dashboard are shown for that team. configuration
 for each part of the dashboard. relevant repos. relevant notion databases. etc.
- there should be a "discovery" step where sources are analyzed and catalogued, and
then in the configuration section catalogued sources can be used. For example,
cataloguing a github repo can yield .md files as documentation sources like the
4-week sprint plan, which could then be specified as an input into the goals and
concerns processing pipelines.

Workflow:
- user will load the dashboard, tweak sources to give feedback, reload dashboard -
until the dashboard matches what they think the true status is. so we need to think
about what this feedback loop will be like.
- we want to use AI reasoning heavily but apply it smartly to prevent re-processing
the same info over and over, and also to avoid wildly fluctuating outputs from the
same inputs.
- It needs to be possible to refresh only 1 team at a time OR the entire org-level
dashboard
- We need an auth system so that only registered users can see data

I want to build this so that we have a backend server to support either a TUI or a
web frontend. To start, I want to build the TUI.
