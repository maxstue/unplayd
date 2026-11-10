import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { listLibraryPlatforms } from "../../bff/handlers";

export default defineEventHandler((event) => listLibraryPlatforms(event, useRuntimeConfig(event)));
