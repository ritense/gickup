package onedev

import (
	"fmt"
	"time"

	"github.com/cooperspencer/gickup/types"
	"github.com/cooperspencer/onedev"
	"github.com/rs/zerolog/log"
)

func Get(conf *types.Conf) ([]types.Repo, bool) {
	ran := false
	repos := []types.Repo{}

	for _, repo := range conf.Source.OneDev {
		ran = true
		if repo.URL == "" {
			repo.URL = "https://code.onedev.io/"
		}
		err := repo.Filter.ParseDuration()
		if err != nil {
			log.Error().
				Str("stage", "onedev").
				Str("url", repo.URL).
				Msg(err.Error())
		}
		include := types.GetMap(repo.Include)
		exclude := types.GetMap(repo.Exclude)
		excludeorgs := types.GetMap(repo.ExcludeOrgs)

		log.Info().
			Str("stage", "onedev").
			Str("url", repo.URL).
			Msgf("grabbing repositories from %s", repo.User)

		if repo.Password == "" && repo.Token != "" {
			repo.Password = repo.Token
		}

		client := &onedev.Client{}

		if repo.Token != "" || repo.TokenFile != "" {
			client = onedev.NewClient(repo.URL, onedev.SetToken(repo.GetToken()))
		} else {
			if repo.Password != "" {
				client = onedev.NewClient(repo.URL, onedev.SetBasicAuth(repo.Username, repo.Password))
			} else {
				client = onedev.NewClient(repo.URL)
			}
		}

		query := onedev.ProjectQueryOptions{
			Query:  "",
			Offset: 0,
			Count:  100,
		}

		user := onedev.User{}

		if repo.User == "" {
			u, _, err := client.GetMe()
			if err != nil {
				log.Error().
					Str("stage", "onedev").
					Str("url", repo.URL).
					Msg("can't find user")
				break
			}
			user = u
			repo.User = user.Name
		}

		if repo.User != "" {
			query.Query = fmt.Sprintf("owned by \"%s\"", repo.User)
		}

		userrepos, _, err := client.GetProjects(&query)
		if err != nil {
			log.Error().
				Str("stage", "onedev").
				Str("url", repo.URL).
				Msg(err.Error())
		}

		for _, r := range userrepos {
			if repo.Filter.ExcludeForks {
				if r.ForkedFromID != 0 {
					continue
				}
			}
			if len(repo.Include) > 0 {
				if !include[r.Name] {
					continue
				}
				if exclude[r.Name] {
					continue
				}
			}

			urls, _, err := client.GetCloneUrl(r.ID)
			if err != nil {
				log.Error().
					Str("stage", "onedev").
					Str("url", repo.URL).
					Msg("couldn't get clone urls")
				continue
			}

			defaultbranch, _, err := client.GetDefaultBranch(r.ID)
			if err != nil {
				log.Error().
					Str("stage", "onedev").
					Str("url", repo.URL).
					Msgf("couldn't get default branch for %s", r.Name)
				defaultbranch = "main"
			}

			options := onedev.CommitQueryOptions{Query: fmt.Sprintf("branch(%s)", defaultbranch)}
			commits, _, err := client.GetCommits(r.ID, &options)
			if len(commits) > 0 {
				commit, _, err := client.GetCommit(r.ID, commits[0])
				if err != nil {
					log.Error().
						Str("stage", "onedev").
						Str("url", repo.URL).
						Msgf("can't get latest commit for %s", defaultbranch)
				} else {
					lastactive := time.UnixMicro(commit.Author.When)
					if time.Since(lastactive) > repo.Filter.LastActivityDuration && repo.Filter.LastActivityDuration != 0 {
						continue
					}
				}
			}

			repos = append(repos, types.Repo{
				Name:          r.Name,
				URL:           urls.HTTP,
				SSHURL:        urls.SSH,
				Token:         repo.Token,
				Defaultbranch: defaultbranch,
				Origin:        repo,
				Owner:         repo.User,
				Hoster:        types.GetHost(repo.URL),
				Description:   r.Description,
			})
		}

		if repo.Username != "" && repo.Password != "" && len(repo.IncludeOrgs) == 0 && user.Name != "" {
			memberships, _, err := client.GetUserMemberships(user.ID)
			if err != nil {
				log.Error().
					Str("stage", "onedev").
					Str("url", repo.URL).
					Msgf("couldn't get memberships for %s", user.Name)
			}

			for _, membership := range memberships {
				group, _, err := client.GetGroup(membership.GroupID)
				if err != nil {
					log.Error().
						Str("stage", "onedev").
						Str("url", repo.URL).
						Msgf("couldn't get group with id %d", membership.GroupID)
				}
				if !excludeorgs[group.Name] {
					repo.IncludeOrgs = append(repo.IncludeOrgs, group.Name)
				}
			}
		}

		if len(repo.IncludeOrgs) > 0 {
			for _, org := range repo.IncludeOrgs {
				query.Query = fmt.Sprintf("children of \"%s\"", org)

				orgrepos, _, err := client.GetProjects(&query)
				if err != nil {
					log.Error().
						Str("stage", "onedev").
						Str("url", repo.URL).
						Msg(err.Error())
				}

				for _, r := range orgrepos {
					if repo.Filter.ExcludeForks {
						if r.ForkedFromID != 0 {
							continue
						}
					}
					urls, _, err := client.GetCloneUrl(r.ID)
					if err != nil {
						log.Error().
							Str("stage", "onedev").
							Str("url", repo.URL).
							Msg("couldn't get clone urls")
						continue
					}

					defaultbranch, _, err := client.GetDefaultBranch(r.ID)
					if err != nil {
						log.Error().
							Str("stage", "onedev").
							Str("url", repo.URL).
							Msgf("couldn't get default branch for %s", r.Name)
						defaultbranch = "main"
					}

					repos = append(repos, types.Repo{
						Name:          r.Name,
						URL:           urls.HTTP,
						SSHURL:        urls.SSH,
						Token:         repo.Token,
						Defaultbranch: defaultbranch,
						Origin:        repo,
						Owner:         org,
						Hoster:        types.GetHost(repo.URL),
						Description:   r.Description,
					})
				}
			}
		}
	}

	return repos, ran
}

func GetOrCreate(destination types.GenRepo, repo types.Repo) (string, error) {
	client := &onedev.Client{}
	if destination.URL == "" {
		destination.URL = "https://code.onedev.io/"
	}

	if destination.Token != "" || destination.TokenFile != "" {
		client = onedev.NewClient(destination.URL, onedev.SetToken(destination.GetToken()))
	} else {
		if destination.Password != "" {
			client = onedev.NewClient(destination.URL, onedev.SetBasicAuth(destination.Username, destination.Password))
		} else {
			client = onedev.NewClient(destination.URL, "", "")
		}
	}

	user, _, err := client.GetMe()
	if err != nil {
		return "", err
	}

	query := onedev.ProjectQueryOptions{
		Query:  fmt.Sprintf("\"Name\" is \"%s\" and children of \"%s\"", repo.Name, user.Name),
		Offset: 0,
		Count:  100,
	}
	projects, _, err := client.GetProjects(&query)

	if err != nil {
		return "", err
	}

	for _, project := range projects {
		if project.Name == repo.Name {
			cloneUrls, _, err := client.GetCloneUrl(project.ID)
			if err != nil {
				return "", err
			}
			return cloneUrls.HTTP, nil
		}
	}

	query.Query = fmt.Sprintf("\"Name\" is \"%s\"", user.Name)

	parentid := 0
	parents, _, err := client.GetProjects(&query)
	if err != nil {
		return "", err
	}
	for _, parent := range parents {
		if parent.Name == user.Name {
			parentid = parent.ID
		}
	}

	project, _, err := client.CreateProject(&onedev.CreateProjectOptions{Name: repo.Name, ParentID: parentid, CodeManagement: true})
	if err != nil {
		return "", err
	}

	cloneUrls, _, err := client.GetCloneUrl(project)
	if err != nil {
		return "", err
	}

	return cloneUrls.HTTP, nil
}
