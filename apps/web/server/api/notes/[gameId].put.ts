import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { setNote } from "../../bff/handlers";

export default defineEventHandler((event) => setNote(event, useRuntimeConfig(event)));
