import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { deleteLibraryEntry } from "../../bff/handlers";

export default defineEventHandler((event) => deleteLibraryEntry(event, useRuntimeConfig(event)));
