package main

import (
	"fmt"
	"regexp"
	"sync"
)

type ClientParty struct {
	ID      string `json:"id"`
	Match   string `json:"match"`
	Session string `json:"session"`
	Leader  bool   `json:"leader"`
}

type Party struct {
	ID      string
	Match   string
	Session string
	Members []string
}

type PartyStore struct {
	parties []*Party
	mu      sync.Mutex
}

var idRegex = regexp.MustCompile(`^[0-9a-f]{32}$`)
var partyIDRegex = regexp.MustCompile(`^V2:[0-9a-f]{32}$`)

func (clientParty *ClientParty) Validate() bool {
	if !partyIDRegex.MatchString(clientParty.ID) {
		return false
	}

	if !idRegex.MatchString(clientParty.Match) {
		return false
	}

	if !idRegex.MatchString(clientParty.Session) {
		return false
	}

	return true
}

func (store *PartyStore) RemoveUser(user string) bool {
	store.mu.Lock()
	defer store.mu.Unlock()

	for i, party := range store.parties {
		for j, member := range party.Members {
			if user == member {
				if j == 0 {
					// Delete party
					store.parties = append(store.parties[:i], store.parties[i+1:]...)
				} else {
					party.Members = append(party.Members[:j], party.Members[j+1:]...)
				}

				return true
			}
		}
	}

	return false
}

func (store *PartyStore) CreateOrJoinParty(user string, clientParty ClientParty) (*Party, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	for _, party := range store.parties {
		if party.ID == clientParty.ID && party.Match == clientParty.Match && party.Session == clientParty.Session {
			if clientParty.Leader {
				return nil, fmt.Errorf("party already exists")
			}

			party.Members = append(party.Members, user)

			return party, nil
		}
	}

	if !clientParty.Leader {
		return nil, fmt.Errorf("party doesn't exist")
	}

	party := &Party{
		ID:      clientParty.ID,
		Match:   clientParty.Match,
		Session: clientParty.Session,
		Members: []string{user},
	}

	store.parties = append(store.parties, party)

	return party, nil
}

func (store *PartyStore) GetUserParty(user string) *Party {
	store.mu.Lock()
	defer store.mu.Unlock()

	for _, party := range store.parties {
		for _, member := range party.Members {
			if member == user {
				return party
			}
		}
	}

	return nil
}
