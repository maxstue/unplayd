import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { removeNote } from "../../bff/handlers";

export default defineEventHandler((event) => removeNote(event, useRuntimeConfig(event)));
