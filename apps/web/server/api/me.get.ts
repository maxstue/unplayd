import { currentUser } from "../auth/current-user";
import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";

export default defineEventHandler((event) => currentUser(event, useRuntimeConfig(event)));
