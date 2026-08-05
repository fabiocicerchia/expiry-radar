# Architecture

<!-- Diagram or bullet list of the main components and how they interact. -->

## Overview

## Components

## Data flow

## Decisions

Record significant choices here (or in a `docs/adr/` folder if they pile up).

## How the ranking works

`priority = 0.55 × urgency + 0.45 × blast radius`, sorted descending.

Urgency ramps linearly from 0 at ninety days out to 1 on the expiry date, and
stays at 1 once expired. It is a **weighted sum, not a product**, on purpose: a
product ranks everything beyond the horizon at exactly zero and throws away the
ordering that makes the tool worth running.

Blast radius starts from the kind (a domain or an intermediate CA outranks a
leaf certificate, because it takes out everything below it) and is then moved by
whatever evidence exists: internet-facing, production vs non-production
namespace, wildcard or multi-SAN coverage, reported traffic, and whether an ACM
certificate is in use at all. Environment detection matches whole tokens, so
`device-registry` is not "dev" and `reproduction-service` is not "prod".

Two things override inference, in order: an `expiry-radar/blast-radius` label on
the resource, then an operator `overrides` glob in the config. Inference exists
because most estates have no reliable labels — not because it knows better.

Every row carries a `WHY` column. A ranking nobody can explain gets ignored.
