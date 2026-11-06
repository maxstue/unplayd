import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { listLibrary } from "../../bff/handlers";

export default defineEventHandler((event) => listLibrary(event, useRuntimeConfig(event)));
