import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { getLibraryEntry } from "../../bff/handlers";

export default defineEventHandler((event) => getLibraryEntry(event, useRuntimeConfig(event)));
