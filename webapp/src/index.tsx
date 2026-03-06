// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import manifest from "manifest";
import type { Store } from "redux";

import type { GlobalState } from "@mattermost/types/store";
import { Client4 } from "mattermost-redux/client";
import { getCurrentChannelId } from "mattermost-redux/selectors/entities/common";

import type { PluginRegistry } from "types/mattermost-webapp";

import React from "react";

const MenuIcon = () => <i className="icon fa fa-video-camera" />;

const AUTH_COMPLETE_MESSAGE_TYPE = "google-meet-auth-complete";
const AUTH_COMPLETE_STORAGE_KEY = `${manifest.id}_auth_complete`;
const PENDING_CHANNEL_STORAGE_KEY = `${manifest.id}_pending_channel_id`;

const HeaderIcon = () => (
    <i
        className="icon fa fa-video-camera"
        style={{ fontSize: "15px", position: "relative", top: "-1px" }}
    />
);

const createMeeting = async (
    channelId: string,
    allowAuthRedirect = true,
) => {
    const response = await fetch(
        `/plugins/${manifest.id}/api/v1/create?channel_id=${encodeURIComponent(channelId)}`,
        Client4.getOptions({
            method: "POST",
            headers: {
                "Content-Type": "application/json",
            },
        }),
    );

    if (response.status === 401) {
        if (allowAuthRedirect) {
            window.localStorage.setItem(PENDING_CHANNEL_STORAGE_KEY, channelId);
            window.open(`/plugins/${manifest.id}/oauth2/login`, "_blank");
        }
        return false;
    }

    if (!response.ok) {
        throw new Error("Failed to create meeting");
    }

    const data = await response.json();
    window.open(data.meet_url, "_blank");
    return true;
};

export default class Plugin {
    // eslint-disable-next-line @typescript-eslint/no-unused-vars, @typescript-eslint/no-empty-function
    public async initialize(
        registry: PluginRegistry,
        store: Store<GlobalState>,
    ) {
        let retryInProgress = false;

        const retryPendingMeeting = async () => {
            if (retryInProgress) {
                return;
            }

            const pendingChannelId = window.localStorage.getItem(
                PENDING_CHANNEL_STORAGE_KEY,
            );
            if (!pendingChannelId) {
                return;
            }

            window.localStorage.removeItem(PENDING_CHANNEL_STORAGE_KEY);
            retryInProgress = true;

            try {
                await createMeeting(pendingChannelId, false);
            } catch (error) {
                // eslint-disable-next-line no-console
                console.error("Error creating meeting after auth:", error);
            } finally {
                retryInProgress = false;
            }
        };

        window.addEventListener("message", (event: MessageEvent) => {
            const data = event.data;
            if (
                data?.type === AUTH_COMPLETE_MESSAGE_TYPE &&
                data?.pluginId === manifest.id
            ) {
                void retryPendingMeeting();
            }
        });

        window.addEventListener("storage", (event: StorageEvent) => {
            if (
                event.key === AUTH_COMPLETE_STORAGE_KEY &&
                event.newValue
            ) {
                void retryPendingMeeting();
            }
        });

        const handleCreateMeeting = async () => {
            const channelId = getCurrentChannelId(store.getState());
            if (!channelId) {
                return;
            }

            try {
                await createMeeting(channelId);
            } catch (error) {
                // eslint-disable-next-line no-console
                console.error("Error creating meeting:", error);
            }
        };

        const handleChannelMenuMeeting = async (channelId: string) => {
            if (!channelId) {
                return;
            }

            try {
                await createMeeting(channelId);
            } catch (error) {
                // eslint-disable-next-line no-console
                console.error("Error creating meeting:", error);
            }
        };

        registry.registerChannelHeaderButtonAction(
            <HeaderIcon />,
            handleCreateMeeting,
            "Google Meet",
            "Create Google Meet link",
        );

        registry.registerChannelHeaderMenuAction(
            "Create Google Meet",
            handleChannelMenuMeeting,
        );

        registry.registerMainMenuAction(
            "Create Google Meet",
            handleCreateMeeting,
            <MenuIcon />,
        );
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
