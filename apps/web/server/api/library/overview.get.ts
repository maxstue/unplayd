import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { getLibraryOverview } from "../../bff/handlers";

export default defineEventHandler((event) => getLibraryOverview(event, useRuntimeConfig(event)));
