import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { addLibraryEntry } from "../../bff/handlers";

export default defineEventHandler((event) => addLibraryEntry(event, useRuntimeConfig(event)));
