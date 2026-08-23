"""Centralt lager för användarsynlig svensk TUI-text."""

from __future__ import annotations

import random


TEXT = {
    "app_title": "BACKFLASH // DISKURSÖVERVAKNING",
    "app_subtitle": "lokal terminalklient",
    "threads": "TRÅDAR",
    "posts": "INLÄGG",
    "details": "DETALJER",
    "forums": "FORUM",
    "forum_root": "FORUM · FLASHBACK",
    "unknown_time": "okänd tid",
    "unknown_user": "okänd användare",
    "unknown_forum": "okänt forum",
    "untitled": "utan titel",
    "empty": "inget valt.\n\nmänskligheten väntar.",
    "cache_warm": "HUND VAKEN // CACHE VARM",
    "select_thread": "Välj en tråd för att börja.",
    "no_posts": "Inga inlägg matchar filtret.",
    "no_cached_matches": "Inga sparade inlägg matchade",
    "search_remote": "SÖK PÅ FLASHBACK",
    "search_local": "SÖK LOKALT",
    "search_placeholder": "sökord",
    "search_submit": "Enter sök · Escape avbryt",
    "searching": "Söker på Flashback…",
    "remote_result_hint": "Välj ett resultat för att öppna eller hämta tråden.",
    "no_remote_results": "Inga fjärrresultat.",
    "search_failed": "Fjärrsökningen misslyckades; lokalt innehåll finns kvar",
    "navigation_failed": "Navigeringen misslyckades; sparad data finns kvar",
    "loading_forum": "Hämtar trådlista…",
    "ingesting": "Hämtar tråden…",
    "thread_ready": "Tråden är klar",
    "no_thread_url": "Trådlistan saknar användbar URL.",
    "remote_no_thread": "Fjärrresultatet saknar en tråd som kan öppnas.",
    "sync_hint": "Använd synkroniseringskommandot för att uppdatera tråden.",
    "post": "INLÄGG",
    "original": "ORIGINALTEXT",
    "quoted": "CITERAT INNEHÅLL",
    "source_page": "KÄLLSIDA",
    "position": "POSITION",
    "ingest_failed": "Kunde inte hämta tråden",
}

STARTUP_QUOTES = (
    "Ännu en tråd. Ännu ett misstag.",
    "Du skulle ha gått och lagt dig.",
    "Internet var ett misstag. Fortsätter ändå.",
    "Det här kändes viktigare klockan 03:47.",
    "Källkritik först. Personangrepp sedan.",
    "BACKFLASH startar. Kaffet borde ha gjort detsamma.",
    "Det finns nya inlägg. Tyvärr.",
    "Mänskligheten kunde inte nås. Försöker igen.",
)


def text(key: str, **values: object) -> str:
    value = TEXT[key]
    return value.format(**values) if values else value


def startup_quote() -> str:
    return random.choice(STARTUP_QUOTES)
